package registration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestUnwatchWaitsForActiveReconciliation(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockState()
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (&Service{}).SetManagement(ctx, "scope", "sandbox", store.Unwatched) }()
	select {
	case err := <-done:
		t.Fatalf("Unwatch failed during reconciliation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Unwatch never completed after reconciliation")
	}
	choices, err := store.ReadManagement()
	if err != nil || choices[store.StatusKey("scope", "sandbox")] != store.Unwatched {
		t.Fatalf("Unwatch was not saved: %+v %v", choices, err)
	}
}

func TestUnwatchStopsWaitingWhenContextExpires(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockState()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err = (&Service{}).SetManagement(ctx, "scope", "sandbox", store.Unwatched)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wait deadline, got %v", err)
	}
	choices, err := store.ReadManagement()
	if err != nil || choices[store.StatusKey("scope", "sandbox")] != "" {
		t.Fatalf("expired Unwatch changed management: %+v %v", choices, err)
	}
}

func TestDiscoveryPreservesChoicesAndManualRegistrations(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sandboxes := []sbx.Sandbox{{ID: "new", Name: "new", Status: "pending"}, {ID: "manual", Name: "manual", Status: "running"}, {ID: "off", Name: "off", Status: "running"}}
	if err := store.SetManagement("scope", "off", store.Disabled); err != nil {
		t.Fatal(err)
	}
	machines := []herdr.Machine{{ID: "manual-machine", Label: "Release desk", Target: "manual.sbx", Session: "default"}}
	for range 2 {
		rows, err := observeManagement("scope", sandboxes, machines, store.State{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 || rows[0].Management != store.Watched || rows[1].Management != store.Unmanaged || rows[1].Label != "Release desk" || rows[2].Management != store.Disabled {
			t.Fatalf("rows=%+v", rows)
		}
	}
	choices, err := store.ReadManagement()
	if err != nil {
		t.Fatal(err)
	}
	if choices[store.StatusKey("scope", "manual")] != "" {
		t.Fatal("manual registration became watched")
	}
}

func TestPresentStoppedSandboxClearsDeletionUncertainty(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sb := sbx.Sandbox{ID: "new", Name: "new", Status: "stopped"}
	if err := store.RecordSandboxStatus("scope", sb, "not checked", "sandbox deletion recovery is unavailable", true); err != nil {
		t.Fatal(err)
	}
	rows, err := observeManagement("scope", []sbx.Sandbox{sb}, nil, store.State{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Management != store.Watched || rows[0].Status.Stale || strings.Contains(rows[0].Status.Detail, "deletion recovery") {
		t.Fatalf("present stopped row retains unrelated deletion uncertainty: %+v", rows)
	}
}

func TestDisablePersistsOptOutOnFailureAndRetriesOwnedRemoval(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Label: "desk", Target: "demo.sbx", CustomLabel: true}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}); err != nil {
		t.Fatal(err)
	}
	fail, present := true, true
	removes := 0
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "herdr machine list --json":
			if present {
				return `[{"id":"machine","label":"desk","target":"demo.sbx","session":"default"}]`, nil
			}
			return `[]`, nil
		case "herdr machine remove machine":
			removes++
			if fail {
				return "offline", errors.New("offline")
			}
			present = false
			return "", nil
		default:
			return "", fmt.Errorf("unexpected %s", call)
		}
	}
	f.rebuildClients()
	if err := f.service.SetManagement(context.Background(), "scope", "one", store.Disabled); err == nil {
		t.Fatal("hidden removal failure")
	}
	choices, err := store.ReadManagement()
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if choices[store.StatusKey("scope", "one")] != store.Disabled || len(state.Scopes["scope"]) != 1 {
		t.Fatal("lost opt-out or retry ownership")
	}
	rows, err := observeManagement("scope", []sbx.Sandbox{{ID: "one", Name: "demo", Status: "running"}}, []herdr.Machine{{ID: "machine", Label: "desk", Target: "demo.sbx", Session: "default"}}, state)
	if err != nil || len(rows) != 1 || rows[0].Management != store.Unwatched || !rows[0].Owned {
		t.Fatalf("failed disable presentation=%+v err=%v", rows, err)
	}
	fail = false
	if err := f.service.SetManagement(context.Background(), "scope", "one", store.Disabled); err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Scopes["scope"]) != 0 || removes != 2 {
		t.Fatalf("state=%+v removes=%d", state, removes)
	}
}

func TestCustomLabelSurvivesReconciliationAndUnwatchRetainsDeletedRegistration(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Label: "Release desk", Target: "demo.sbx", CustomLabel: true}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	machines := []herdr.Machine{{ID: "machine", Label: "Release desk", Target: "demo.sbx", Session: "default"}}
	if err := f.service.recoverMachineLabel(context.Background(), "scope", &state, machines, sbx.Sandbox{ID: "one", Name: "demo"}, &item, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if item.Label != "Release desk" {
		t.Fatalf("label=%q", item.Label)
	}
	if err := store.SetManagement("scope", "one", store.Unwatched); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RemoveDeletedSandbox(context.Background(), "scope", "socket", "one"); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil || len(state.Scopes["scope"]) != 1 {
		t.Fatalf("ownership=%+v err=%v", state, err)
	}
}

func TestRenameRetryCanReplaceUncertainLabelAndPreserveOwnership(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Label: "original", Target: "demo.sbx", CustomLabel: true}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}); err != nil {
		t.Fatal(err)
	}
	actual := "original"
	renames := 0
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		if call == "herdr machine list --json" {
			return fmt.Sprintf(`[{"id":"machine","label":%q,"target":"demo.sbx","session":"default"}]`, actual), nil
		}
		if strings.HasPrefix(call, "herdr machine rename machine --label ") {
			renames++
			actual = command.Args[len(command.Args)-1]
			if renames == 1 {
				return "", errors.New("lost acknowledgement")
			}
			return "", nil
		}
		return "", fmt.Errorf("unexpected %s", call)
	}
	f.rebuildClients()
	if err := f.service.Rename(context.Background(), "scope", "one", "first"); err == nil {
		t.Fatal("hidden uncertain rename")
	}
	if err := f.service.Rename(context.Background(), "scope", "one", "corrected"); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	saved := state.Scopes["scope"][0]
	if saved.Label != "corrected" || saved.PendingLabel != "" || !saved.CustomLabel || actual != "corrected" {
		t.Fatalf("saved=%+v actual=%q", saved, actual)
	}
}

func TestInventoryOmissionMarksCacheStaleAndPreservesChoices(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sandbox := sbx.Sandbox{ID: "gone", Name: "demo", Status: "running"}
	if _, err := observeManagement("scope", []sbx.Sandbox{sandbox}, nil, store.State{}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetManagement("scope", "gone", store.Unwatched); err != nil {
		t.Fatal(err)
	}
	if _, err := observeManagement("scope", nil, nil, store.State{}); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	cached := status.Sandboxes[store.StatusKey("scope", "gone")]
	if !cached.Stale || !strings.Contains(cached.Detail, "deletion is unconfirmed") || status.Management[store.StatusKey("scope", "gone")] != store.Unwatched {
		t.Fatalf("cache=%+v choices=%+v", cached, status.Management)
	}
}
