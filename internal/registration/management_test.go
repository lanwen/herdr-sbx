package registration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func boolPointer(v bool) *bool { return &v }

func newManagementFixture(t *testing.T, runtime string) (*registrationFixture, string, *[]herdr.Machine, *[]string) {
	t.Helper()
	f := newRegistrationFixture(t, fmt.Sprintf(`[{"uuid":"one","name":"demo","status":%q}]`, runtime))
	machines := []herdr.Machine{}
	calls := []string{}
	f.runner.Execute = func(_ context.Context, c process.Command) (string, error) {
		call := c.Name + " " + strings.Join(c.Args, " ")
		calls = append(calls, call)
		switch call {
		case "herdr --version":
			return "herdr 0.9.3", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/test-sandboxd.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "herdr machine list --json":
			raw, _ := json.Marshal(machines)
			return string(raw), nil
		case "herdr machine disable machine":
			machines[0].Enabled = boolPointer(false)
			return "", nil
		case "herdr machine enable machine":
			machines[0].Enabled = boolPointer(true)
			return "", nil
		case "herdr machine remove machine":
			machines = []herdr.Machine{}
			return "", nil
		}
		if strings.HasPrefix(call, "herdr machine rename machine --label ") {
			machines[0].Label = c.Args[len(c.Args)-1]
			return "", nil
		}
		return "", fmt.Errorf("unexpected command: %s", call)
	}
	f.rebuildClients()
	return f, store.ScopeID("/tmp/test-sandboxd.sock"), &machines, &calls
}

func TestNewDiscoveryDefaultsEnabledAndRemembersDisabledWithoutEntry(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sb := sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}
	for _, choice := range []store.EnabledChoice{store.Enabled, store.Disabled} {
		if choice == store.Disabled {
			if err := store.SetEnabled("scope", "one", choice); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := observeManagement("scope", []sbx.Sandbox{sb}, nil, store.State{})
		if err != nil || len(rows) != 1 || rows[0].Enabled != choice || rows[0].EntryPresent {
			t.Fatalf("rows=%+v error=%v", rows, err)
		}
	}
}

func TestAssociationUsesStableIdentityAndNotLabel(t *testing.T) {
	sb := sbx.Sandbox{ID: "one", Name: "demo"}
	state := store.State{Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "old"}}}}
	m := herdr.Machine{ID: "machine", Label: "external label", Target: "demo.sbx", Session: "default"}
	if found, issue := associatedMachine("scope", sb, []herdr.Machine{m}, state); found == nil || issue != "" {
		t.Fatalf("rename broke association: %v %s", found, issue)
	}
	for _, other := range []herdr.Machine{{ID: "new", Target: "demo.sbx", Session: "default"}, {ID: "machine", Target: "demo.sbx", Session: "other"}} {
		if found, issue := associatedMachine("scope", sb, []herdr.Machine{other}, state); found != nil || issue == "" {
			t.Fatalf("unsafe association: %v %s", found, issue)
		}
	}
	if found, issue := associatedMachine("scope", sb, []herdr.Machine{m, m}, state); found != nil || issue == "" {
		t.Fatal("duplicate target accepted")
	}
}

func TestStoppedEnableWithoutEntryStaysDisabledQuietly(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "stopped")
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	if err != nil {
		t.Fatalf("error=%v", err)
	}
	choices, _ := store.ReadEnabled()
	if choices[store.StatusKey(scope, "one")] != store.Disabled {
		t.Fatal("unsupported enable was retained")
	}
	for _, call := range *calls {
		if strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") || strings.Contains(call, "sbx exec") {
			t.Fatalf("wake-capable command: %s", call)
		}
	}
	status, _ := store.ReadStatusStore()
	if len(status.Errors) != 0 {
		t.Fatalf("unexpected diagnostic: %+v", status.Errors)
	}
	if row := status.Sandboxes[store.StatusKey(scope, "one")]; row.Connection != "stopped" || row.Enabled != store.Disabled {
		t.Fatalf("unsupported stopped state: %+v", row)
	}
}

func TestDisableRemoveEnablePreservesDisabledAndObservedLabel(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Disabled); err != nil {
		t.Fatal(err)
	}
	rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
	if err != nil || !rows[0].EntryPresent || rows[0].Label != "Desk" || rows[0].NativeEnabled == nil || *rows[0].NativeEnabled {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := f.service.RemoveEntry(context.Background(), scope, "one"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	choices, _ := store.ReadEnabled()
	if choices[store.StatusKey(scope, "one")] != store.Disabled {
		t.Fatal("removed sandbox rediscovered as enabled")
	}
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err != nil {
		t.Fatalf("unsafe re-enable: %v", err)
	}
}

func TestRemoveFailureKeepsEntryAndRetry(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	if err := store.SetEnabled(scope, "one", store.Disabled); err != nil {
		t.Fatal(err)
	}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine remove machine" {
			return "offline", errors.New("offline")
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	if err := f.service.RemoveEntry(context.Background(), scope, "one"); err == nil {
		t.Fatal("hidden failure")
	}
	status, _ := store.ReadStatusStore()
	row := status.Sandboxes[store.StatusKey(scope, "one")]
	if row.Label != "Desk" || !row.EntryPresent {
		t.Fatalf("lost actual entry: %+v", row)
	}
}

func TestBaselineAcceptsExternalNativeStateAndLabelWhileStopped(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Old", PendingLabel: "cancelled", LastObservedNativeEnabled: boolPointer(true)}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEnabled(scope, "one", store.Enabled); err != nil {
		t.Fatal(err)
	}
	if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	choices, _ := store.ReadEnabled()
	state, _ := store.LoadState()
	saved := state.Scopes[scope][0]
	if choices[store.StatusKey(scope, "one")] != store.Disabled || saved.Label != "External" || saved.PendingLabel != "" {
		t.Fatalf("baseline choice=%v state=%+v", choices, saved)
	}
	(*machines)[0].Enabled = boolPointer(true)
	if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	choices, _ = store.ReadEnabled()
	if choices[store.StatusKey(scope, "one")] != store.Enabled {
		t.Fatal("external native enable was not accepted")
	}
}

func TestRenameWorksWhileDisabledAndManagementOff(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Old", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	_ = store.SetWorkerControl(true)
	if err := f.service.Rename(context.Background(), scope, "one", "New"); err != nil {
		t.Fatal(err)
	}
	if (*machines)[0].Label != "New" {
		t.Fatal("rename not applied")
	}
}

func TestManagementOffRejectsAutomaticWorkBeforeObservation(t *testing.T) {
	f, _, _, calls := newManagementFixture(t, "running")
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReconcileContext(context.Background(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("automatic work was not cancelled: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("OFF issued commands: %v", *calls)
	}
}

func TestUnconfirmedNativeDisableCannotAuthorizeRemoval(t *testing.T) {
	for _, native := range []*bool{nil, boolPointer(true)} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "running")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: native}}
			_ = store.SetEnabled(scope, "one", store.Disabled)
			if err := f.service.RemoveEntry(context.Background(), scope, "one"); err == nil {
				t.Fatal("unconfirmed disable accepted")
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine remove") {
					t.Fatalf("unsafe remove: %s", call)
				}
			}
		})
	}
}

func TestCatalogFailurePreservesLastKnownLabel(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	if _, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine list --json" {
			return "unavailable", errors.New("unavailable")
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
	if err == nil || len(rows) != 1 || rows[0].Label != "Desk" || !rows[0].EntryPresent || !rows[0].Status.Stale || rows[0].AssociationIssue == "" {
		t.Fatalf("rows=%+v err=%v scope=%s", rows, err, scope)
	}
}

func TestPartialRenameAcknowledgementUsesActualResult(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Old", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		out, err := execute(ctx, c)
		if strings.HasPrefix(strings.Join(c.Args, " "), "machine rename") {
			return "response lost", errors.New("response lost")
		}
		return out, err
	}
	f.rebuildClients()
	if err := f.service.Rename(context.Background(), scope, "one", "New"); err != nil {
		t.Fatal(err)
	}
	state, _ := store.LoadState()
	if state.Scopes[scope][0].Label != "New" {
		t.Fatal("actual rename not retained")
	}
}

func TestVerifiedPartialAddRetainsCatalogIdentityOnResume(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Label: "Intent", Pending: true}}}}); err != nil {
		t.Fatal(err)
	}
	_ = store.SetEnabled(scope, "one", store.Enabled)
	if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ReconcileContext(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	state, _ := store.LoadState()
	item := state.Scopes[scope][0]
	if item.ProfileID != "machine" || item.Target != "demo.sbx" || item.Pending || item.Label != "Desk" {
		t.Fatalf("partial add identity lost: %+v", item)
	}
	for _, call := range *calls {
		if strings.Contains(call, "machine add") {
			t.Fatalf("duplicate add: %s", call)
		}
	}
}

func TestCurrentStateReplacesStaleReady(t *testing.T) {
	for _, test := range []struct {
		name, runtime string
		choice        store.EnabledChoice
		present       bool
		native        *bool
		want          string
	}{
		{"disabled present", "running", store.Disabled, true, boolPointer(false), "disabled"},
		{"disabled missing", "running", store.Disabled, false, nil, "disabled"},
		{"stopped enabled entry", "stopped", store.Enabled, true, boolPointer(true), "waiting for start"},
		{"stopped disabled entry", "stopped", store.Enabled, true, boolPointer(false), "disabled in herdr"},
		{"stopped unknown entry", "stopped", store.Enabled, true, nil, "enable not confirmed"},
		{"stopped missing", "stopped", store.Enabled, false, nil, "stopped"},
		{"native disabled", "running", store.Enabled, true, boolPointer(false), "disabled in herdr"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			sb := sbx.Sandbox{ID: "one", Name: "demo", Status: test.runtime}
			_ = store.RecordSandboxStatus("scope", sb, "ready", "old ready", false)
			_ = store.SetEnabled("scope", "one", test.choice)
			var machines []herdr.Machine
			if test.present {
				machines = []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Label: "Desk", Enabled: test.native}}
			}
			rows, err := observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
			if err != nil || rows[0].Status.Connection != test.want || rows[0].Status.Detail == "old ready" {
				t.Fatalf("row=%+v err=%v", rows, err)
			}
		})
	}
}

func TestDisableDuringMissingEntryEnablePreventsAdd(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "running")
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "ssh" && strings.HasSuffix(strings.Join(c.Args, " "), remoteHerdrStatusShell()) {
			if err := store.SetEnabled(scope, "one", store.Disabled); err != nil {
				t.Fatal(err)
			}
			return remoteHerdrOnPath + "\nherdr 0.9.3", nil
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err == nil {
		t.Fatal("cancelled Enable claimed success")
	}
	for _, call := range *calls {
		if strings.Contains(call, "machine add") {
			t.Fatalf("disabled sandbox added: %s", call)
		}
	}
	choices, _ := store.ReadEnabled()
	if choices[store.StatusKey(scope, "one")] != store.Disabled {
		t.Fatal("later Disable lost")
	}
}

func TestDisableDuringFixDownloadPreventsTransferAndHandoff(t *testing.T) {
	f, scope, _, handoffs := newFixHerdrFixture(t, false)
	execute := f.runner.Execute
	transfers := 0
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "ssh" {
			if c.Stdin != nil {
				transfers++
				return "", errors.New("unexpected transfer")
			}
			args := strings.Join(c.Args, " ")
			if strings.HasSuffix(args, remoteHerdrStatusShell()) {
				return remoteHerdrOnPath + "\nherdr 0.9.2", nil
			}
			if strings.HasSuffix(args, "uname -sm") {
				return "Linux x86_64", nil
			}
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	payload := "isolated executable"
	digest := sha256.Sum256([]byte(payload))
	f.service.releaseTransport = releaseTestTransport(func(r *http.Request) (*http.Response, error) {
		body := payload
		if r.URL.String() == releaseManifestURL {
			raw, _ := json.Marshal(herdrRelease{Version: "0.9.3", Assets: map[string]string{"linux-x86_64": "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64"}, SHA256: map[string]string{"linux-x86_64": hex.EncodeToString(digest[:])}})
			body = string(raw)
		} else {
			if err := store.SetEnabled(scope, "sandbox-1", store.Disabled); err != nil {
				t.Fatal(err)
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, "/tmp/fix-herdr.sock", "sandbox-1", "demo", "herdr 0.9.3")
	if !errors.Is(err, context.Canceled) || transfers != 0 || *handoffs != 0 {
		t.Fatalf("fix=%v transfers=%d handoffs=%d", err, transfers, *handoffs)
	}
}

func TestBaselineCannotOverwriteNewerExplicitDisable(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	_ = store.SetEnabled(scope, "one", store.Enabled)
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine list --json" {
			if err := store.SetEnabled(scope, "one", store.Disabled); err != nil {
				t.Fatal(err)
			}
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	choices, _ := store.ReadEnabled()
	if choices[store.StatusKey(scope, "one")] != store.Disabled {
		t.Fatal("baseline overwrote newer Disable")
	}
	state, _ := store.LoadState()
	if state.Scopes[scope][0].Label != "External" {
		t.Fatal("independent current label was lost")
	}
}

func TestLatestEnabledCommandWinsDuringOlderHostObservation(t *testing.T) {
	for _, test := range []struct {
		name         string
		older, newer store.EnabledChoice
		native       bool
	}{
		{"new Disable supersedes Enable", store.Enabled, store.Disabled, false},
		{"new Enable supersedes Disable", store.Disabled, store.Enabled, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "running")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(test.native)}}
			execute := f.runner.Execute
			injected := false
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				if !injected && strings.Join(c.Args, " ") == "machine list --json" {
					injected = true
					if err := store.SetEnabled(scope, "one", test.newer); err != nil {
						t.Fatal(err)
					}
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			err := f.service.SetEnabled(context.Background(), scope, "one", test.older)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("superseded action=%v", err)
			}
			choices, _ := store.ReadEnabled()
			if choices[store.StatusKey(scope, "one")] != test.newer {
				t.Fatal("older action overwrote newer choice")
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine enable") || strings.Contains(call, "machine disable") || strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") {
					t.Fatalf("superseded mutation: %s", call)
				}
			}
		})
	}
}

func TestExplicitEnableKeepsNativeActionWhenRuntimeBecomesStopped(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine list --json" {
			f.inventory = `[{"uuid":"one","name":"demo","status":"stopped"}]`
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err != nil {
		t.Fatal(err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "ssh -o") {
			t.Fatalf("direct guest connection to stopped sandbox: %s", call)
		}
	}
	if !*(*machines)[0].Enabled {
		t.Fatal("explicit Enable did not enable the native connection")
	}
}

func TestSyntheticStatusClearsAfterEnabledRunningObservation(t *testing.T) {
	for _, previous := range []string{"disabled", "waiting for start", "waiting for entry", "disabled in herdr", "disable not confirmed", "workspace setup required"} {
		t.Run(previous, func(t *testing.T) {
			status := store.SandboxStatus{Connection: previous, Detail: "old detail"}
			row := SandboxRow{Enabled: store.Enabled, EntryPresent: true, NativeEnabled: boolPointer(true), Sandbox: sbx.Sandbox{Status: "running"}}
			applyObservedStatus(&status, row)
			if previous == "workspace setup required" {
				if status.Connection != previous || status.Detail != "old detail" {
					t.Fatal("genuine setup failure erased")
				}
			} else if status.Connection != "registered" || status.Detail == "old detail" {
				t.Fatalf("stale synthetic status: %+v", status)
			}
		})
	}
}

func TestUnknownRuntimeIsNeverReportedStopped(t *testing.T) {
	for _, runtime := range []string{"", "unknown", "pending", "failed"} {
		t.Run(runtime, func(t *testing.T) {
			f, scope, _, calls := newManagementFixture(t, runtime)
			var out strings.Builder
			if err := f.service.ReconcileContext(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			status, _ := store.ReadStatusStore()
			row := status.Sandboxes[store.StatusKey(scope, "one")]
			if strings.Contains(strings.ToLower(row.Detail), "stopped") || strings.Contains(out.String(), "cannot add a stopped") {
				t.Fatalf("invented stopped state: %+v\n%s", row, out.String())
			}
			if row.Connection == "ready" || row.Connection == "waiting for start" {
				t.Fatalf("invented confirmed runtime: %+v", row)
			}
			if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err == nil || strings.Contains(err.Error(), "R0008") || strings.Contains(strings.ToLower(err.Error()), "stopped") {
				t.Fatalf("explicit Enable misreported runtime: %v", err)
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") {
					t.Fatalf("unknown runtime connected: %s", call)
				}
			}
		})
	}
}

func TestRemoveEnablePreservesCompletedGuestSetup(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	f.inventory = `[{"uuid":"one","name":"demo","status":"running","workspace":"/workspace/demo"}]`
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	fingerprint, err := guestplugins.DesiredFingerprint(context.Background(), f.service.herdr, scope, "one")
	if err != nil {
		t.Fatal(err)
	}
	checked := time.Now()
	sync := &store.GuestSyncObservation{Fingerprint: "demo.sbx\x00one\x00" + fingerprint, CheckedAt: checked, Status: "ready", Detail: "already registered"}
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk", ProjectWorkspaceDone: true, GuestSync: sync}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}); err != nil {
		t.Fatal(err)
	}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	_ = store.RecordGuestServerObservation(scope, sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}, true, "0.9.3")
	if err := f.service.RemoveEntry(context.Background(), scope, "one"); err != nil {
		t.Fatal(err)
	}
	removed, _ := store.LoadState()
	kept := removed.Scopes[scope][0]
	if kept.ProfileID != "" || !kept.ProjectWorkspaceDone || kept.GuestSync == nil {
		t.Fatalf("guest completion lost on removal: %+v", kept)
	}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		call := c.Name + " " + strings.Join(c.Args, " ")
		switch call {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			*calls = append(*calls, call)
			return remoteHerdrOnPath + "\nherdr 0.9.3", nil
		case "herdr machine add --label sbx · demo demo.sbx":
			*calls = append(*calls, call)
			*machines = []herdr.Machine{{ID: "new-machine", Label: "sbx · demo", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
			return "Saved SSH machine new-machine. Remote server is ready.", nil
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err != nil {
		t.Fatal(err)
	}
	state, _ := store.LoadState()
	restored := state.Scopes[scope][0]
	if !restored.ProjectWorkspaceDone || restored.GuestSync == nil || !restored.GuestSync.CheckedAt.Equal(checked) {
		t.Fatalf("completed guest setup repeated: %+v", restored)
	}
	for _, call := range *calls {
		if strings.Contains(call, "pane list") || strings.Contains(call, "workspace create") || strings.Contains(call, "server start") || strings.Contains(call, "status server --json") {
			t.Fatalf("completed workspace or audit repeated: %s", call)
		}
	}
}

func TestVerifiedAssociationClearsObsoleteConflict(t *testing.T) {
	for _, previous := range []string{"ownership conflict", "association warning"} {
		t.Run(previous, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			sb := sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}
			_ = store.RecordSandboxStatus("scope", sb, previous, "old association warning", false)
			_ = store.SetRegistrationError(store.CodeOwnershipConflict, store.StatusKey("scope", "one"), "old association warning")
			machines := []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Label: "Desk", Enabled: boolPointer(true)}}
			rows, err := observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
			if err != nil || rows[0].Status.Connection != "registered" || rows[0].AssociationIssue != "" {
				t.Fatalf("row=%+v err=%v", rows, err)
			}
			status, _ := store.ReadStatusStore()
			if len(status.Errors) != 0 {
				t.Fatalf("stale conflict: %+v", status.Errors)
			}
			rows, err = observeManagement("scope", []sbx.Sandbox{sb}, append(machines, machines[0]), store.State{})
			if err != nil || rows[0].AssociationIssue != "More than one herdr entry uses this sandbox target." {
				t.Fatalf("real conflict hidden: %+v %v", rows, err)
			}
			rows, err = observeManagement("scope", []sbx.Sandbox{sb}, nil, store.State{})
			if err != nil || rows[0].Status.Connection != "not registered" || rows[0].AssociationIssue != "" {
				t.Fatalf("resolved absent-entry conflict: %+v %v", rows, err)
			}
		})
	}
}

func TestStoppedDiscoveryDoesNotEnableOnLaterStart(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "stopped")
	_ = store.SetRegistrationError(store.CodeSafeEntryUnsupported, store.StatusKey(scope, "one"), "old unsupported entry")
	for _, runtime := range []string{"stopped", "stopped", "running"} {
		f.inventory = fmt.Sprintf(`[{"uuid":"one","name":"demo","status":%q}]`, runtime)
		var out strings.Builder
		if err := f.service.ReconcileContext(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		status, _ := store.ReadStatusStore()
		row := status.Sandboxes[store.StatusKey(scope, "one")]
		if row.Enabled != store.Disabled || len(status.Errors) != 0 {
			t.Fatalf("row=%+v errors=%+v", row, status.Errors)
		}
		if runtime == "stopped" && row.Connection != "stopped" {
			t.Fatalf("row=%+v", row)
		}
	}
	for _, call := range *calls {
		if strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") || strings.Contains(call, "sbx exec") {
			t.Fatalf("unexpected automatic work: %s", call)
		}
	}
}

func TestObservationRetainsCurrentCreationLabelCollision(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sb := sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}
	key := store.StatusKey("scope", "one")
	detail := "Another herdr entry already uses this label. Rename that entry before creating one for this sandbox."
	if err := store.RecordSandboxStatus("scope", sb, "association warning", detail, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRegistrationError(store.CodeOwnershipConflict, key, detail); err != nil {
		t.Fatal(err)
	}
	machines := []herdr.Machine{{ID: "other", Label: sandboxMachineLabel(sb.Name), Target: "other.sbx", Session: "default", Enabled: boolPointer(true)}}
	rows, err := observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	if err != nil || rows[0].Status.Connection != "label already in use" || rows[0].EntryPresent {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	status, err := store.ReadStatusStore()
	if err != nil || len(status.Errors) != 1 {
		t.Fatalf("current creation conflict cleared: %+v %v", status.Errors, err)
	}
	machines = append(machines, herdr.Machine{ID: "machine", Label: "external label", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)})
	rows, err = observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	if err != nil || rows[0].Status.Connection != "registered" || !rows[0].EntryPresent {
		t.Fatalf("new entry did not resolve collision: %+v %v", rows, err)
	}
	machines = machines[:1]
	machines[0].Label = "renamed"
	rows, err = observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	if err != nil || rows[0].Status.Connection != "not registered" {
		t.Fatalf("resolved collision retained: %+v %v", rows, err)
	}
	status, err = store.ReadStatusStore()
	if err != nil || len(status.Errors) != 0 {
		t.Fatalf("resolved diagnostic retained: %+v %v", status.Errors, err)
	}
}

func TestExplicitEnableStoppedExistingEntryEnablesNativeConnection(t *testing.T) {
	for _, managementOff := range []bool{false, true} {
		t.Run(fmt.Sprintf("management_off_%t", managementOff), func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			if err := store.SetWorkerControl(managementOff); err != nil {
				t.Fatal(err)
			}
			if err := store.SetEnabled(scope, "one", store.Disabled); err != nil {
				t.Fatal(err)
			}
			if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err != nil {
				t.Fatal(err)
			}
			if !*(*machines)[0].Enabled {
				t.Fatal("explicit Enable left the native herdr entry disabled")
			}
			if err := store.SetWorkerControl(false); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := f.service.ReconcileContext(context.Background(), &output); err != nil {
				t.Fatal(err)
			}
			if !*(*machines)[0].Enabled {
				t.Fatal("reconciliation undid explicit Enable while sandbox stayed stopped")
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine disable") || strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") || strings.Contains(call, "sbx exec") {
					t.Fatalf("unexpected stopped-sandbox operation: %s", call)
				}
			}
		})
	}
}

func TestStoppedReconciliationPreservesEnabledAndRetriesDisabled(t *testing.T) {
	for _, test := range []struct {
		name         string
		choice       store.EnabledChoice
		native, want bool
	}{
		{"enabled connection stays enabled", store.Enabled, true, true},
		{"disabled connection waits for runtime", store.Enabled, false, false},
		{"disabled choice retries native disable", store.Disabled, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(test.native)}}
			if err := store.SetEnabled(scope, "one", test.choice); err != nil {
				t.Fatal(err)
			}
			if err := f.service.ReconcileContext(context.Background(), io.Discard); err != nil {
				t.Fatal(err)
			}
			if *(*machines)[0].Enabled != test.want {
				t.Fatalf("native enabled=%t, want %t", *(*machines)[0].Enabled, test.want)
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine enable") || strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") || strings.Contains(call, "sbx exec") {
					t.Fatalf("unexpected stopped operation: %s", call)
				}
				if test.choice == store.Enabled && strings.Contains(call, "machine disable") {
					t.Fatalf("Enabled choice was disabled: %s", call)
				}
			}
		})
	}
}

func TestStoppedExplicitEnableVerifiesNativeOutcome(t *testing.T) {
	for _, test := range []struct {
		name                     string
		applied, failed, unknown bool
	}{
		{"rejected", false, true, false},
		{"unconfirmed success", false, false, false},
		{"unknown native state", false, false, true},
		{"lost acknowledgement", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, scope, machines, _ := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				if c.Name == "herdr" && strings.Join(c.Args, " ") == "machine enable machine" {
					if test.applied {
						(*machines)[0].Enabled = boolPointer(true)
					}
					if test.unknown {
						(*machines)[0].Enabled = nil
					}
					if test.failed {
						return "response lost", errors.New("response lost")
					}
					return "", nil
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
			if (err == nil) != test.applied {
				t.Fatalf("applied=%t error=%v", test.applied, err)
			}
			rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
			if err != nil || (rows[0].NativeEnabled == nil) != test.unknown || rows[0].NativeEnabled != nil && *rows[0].NativeEnabled != test.applied {
				t.Fatalf("actual native outcome lost: %+v %v", rows, err)
			}
			if err := f.service.ReconcileContext(context.Background(), io.Discard); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadStatusStore()
			if err != nil {
				t.Fatal(err)
			}
			want := "disabled"
			if test.applied {
				want = "waiting for start"
			} else if test.unknown {
				want = "observation unavailable"
			}
			if rows[0].Status.Connection != want || status.Sandboxes[store.StatusKey(scope, "one")].Connection != want {
				t.Fatalf("native outcome masked: observed=%+v reconciled=%+v", rows[0].Status, status.Sandboxes[store.StatusKey(scope, "one")])
			}
		})
	}
}

func TestNativeToggleSettlesToObservedChoice(t *testing.T) {
	for _, test := range []struct {
		name                        string
		initial, requested, applied bool
	}{
		{"rejected enable", false, true, false},
		{"rejected disable", true, false, false},
		{"applied enable with lost acknowledgement", false, true, true},
		{"applied disable with lost acknowledgement", true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, scope, machines, _ := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(test.initial)}}
			oldChoice, requested := store.Disabled, store.Disabled
			if test.initial {
				oldChoice = store.Enabled
			}
			if test.requested {
				requested = store.Enabled
			}
			_ = store.SetEnabled(scope, "one", oldChoice)
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				if c.Name == "herdr" && (strings.Join(c.Args, " ") == "machine enable machine" || strings.Join(c.Args, " ") == "machine disable machine") {
					if test.applied {
						(*machines)[0].Enabled = boolPointer(test.requested)
					}
					return "request failed", errors.New("request failed")
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			err := f.service.SetEnabled(context.Background(), scope, "one", requested)
			if (err == nil) != test.applied {
				t.Fatalf("applied=%t err=%v", test.applied, err)
			}
			want := oldChoice
			if test.applied {
				want = requested
			}
			choices, _ := store.ReadEnabled()
			if choices[store.StatusKey(scope, "one")] != want {
				t.Fatalf("requested choice retained: choices=%v want=%s", choices, want)
			}
		})
	}
}

func TestUnverifiedNativeToggleWaitsForObservationWithoutReplay(t *testing.T) {
	for _, catalogUnavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("catalog_unavailable_%t", catalogUnavailable), func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			_ = store.SetEnabled(scope, "one", store.Disabled)
			if _, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
				t.Fatal(err)
			}
			mutated := false
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				switch c.Name + " " + strings.Join(c.Args, " ") {
				case "herdr machine enable machine":
					*calls = append(*calls, "herdr machine enable machine")
					mutated = true
					(*machines)[0].Enabled = nil
					return "lost response", errors.New("lost response")
				case "herdr machine list --json":
					if mutated && catalogUnavailable {
						return "catalog unavailable", errors.New("catalog unavailable")
					}
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err == nil {
				t.Fatal("unverified change reported success")
			}
			status, _ := store.ReadStatusStore()
			key := store.StatusKey(scope, "one")
			if status.EnabledChoices[key] != store.Disabled || !status.Sandboxes[key].Stale {
				t.Fatalf("unverified request retained: %+v", status.Sandboxes[key])
			}
			_ = f.service.ReconcileContext(context.Background(), io.Discard)
			enables := 0
			for _, call := range *calls {
				if call == "herdr machine enable machine" {
					enables++
				}
			}
			if enables != 1 {
				t.Fatalf("unverified action replayed: %d", enables)
			}
			catalogUnavailable = false
			(*machines)[0].Enabled = boolPointer(true)
			if _, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
				t.Fatal(err)
			}
			status, _ = store.ReadStatusStore()
			if status.EnabledChoices[key] != store.Enabled || status.Sandboxes[key].Stale {
				t.Fatalf("fresh native state was not adopted: %+v", status.Sandboxes[key])
			}
		})
	}
}

func TestOldCatalogReadCannotResolveNewUnverifiedToggle(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	firstCatalog, mutated := true, false
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		switch c.Name + " " + strings.Join(c.Args, " ") {
		case "herdr machine list --json":
			if firstCatalog {
				firstCatalog = false
				raw, _ := json.Marshal(*machines)
				close(started)
				<-release
				return string(raw), nil
			}
			if mutated {
				return "unavailable", errors.New("unavailable")
			}
		case "herdr machine enable machine":
			mutated = true
			(*machines)[0].Enabled = boolPointer(true)
			return "response lost", errors.New("response lost")
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	go func() {
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		finished <- err
	}()
	<-started
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err == nil {
		t.Fatal("unverified mutation reported success")
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	if !status.EnabledObservationPending[key] || status.EnabledChoices[key] != store.Disabled || !status.Sandboxes[key].Stale {
		t.Fatalf("old catalog resolved new uncertainty: %+v", status.Sandboxes[key])
	}
	mutated = false
	if _, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
		t.Fatal(err)
	}
	status, _ = store.ReadStatusStore()
	if status.EnabledObservationPending[key] || status.EnabledChoices[key] != store.Enabled {
		t.Fatalf("fresh observation not adopted: %+v", status.Sandboxes[key])
	}
}

func TestExplicitAddRequiresVerifiedEnabledEntry(t *testing.T) {
	for _, outcome := range []string{"absent", "disabled", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			f, scope, machines, _ := newManagementFixture(t, "running")
			added, postAddReads := false, 0
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				call := c.Name + " " + strings.Join(c.Args, " ")
				switch call {
				case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
					return remoteHerdrOnPath + "\nherdr 0.9.3", nil
				case "herdr machine add --label sbx · demo demo.sbx":
					added = true
					*machines = []herdr.Machine{{ID: "machine", Label: "sbx · demo", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
					return "Saved SSH machine machine. Remote server is ready.", nil
				case "herdr machine list --json":
					if added {
						postAddReads++
						if postAddReads > 1 {
							switch outcome {
							case "absent":
								*machines = []herdr.Machine{}
							case "disabled":
								(*machines)[0].Enabled = boolPointer(false)
							case "unknown":
								(*machines)[0].Enabled = nil
							}
						}
					}
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err == nil {
				t.Fatal("unverified or disabled entry reported successful Enable")
			}
			status, _ := store.ReadStatusStore()
			key := store.StatusKey(scope, "one")
			if status.EnabledChoices[key] != store.Disabled {
				t.Fatalf("requested choice retained: %+v", status.Sandboxes[key])
			}
			if status.EnabledObservationPending[key] != (outcome == "unknown") {
				t.Fatalf("wrong observation wait: %+v", status.Sandboxes[key])
			}
			if outcome == "unknown" && (!status.Sandboxes[key].EntryPresent || status.Sandboxes[key].Label == "") {
				t.Fatalf("actual added entry lost: %+v", status.Sandboxes[key])
			}
		})
	}
}

func TestOldCatalogCannotChangeConfirmedStateUsedByCancelledToggle(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	firstCatalog := true
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "herdr" && strings.Join(c.Args, " ") == "machine list --json" && firstCatalog {
			firstCatalog = false
			raw, _ := json.Marshal(*machines)
			close(started)
			<-release
			return string(raw), nil
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	go func() {
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		finished <- err
	}()
	<-started
	if err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	if status.Sandboxes[key].NativeEnabled == nil || !*status.Sandboxes[key].NativeEnabled {
		t.Fatalf("old catalog overwrote confirmed native state: %+v", status.Sandboxes[key])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.service.SetEnabled(ctx, scope, "one", store.Disabled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	choices, _ := store.ReadEnabled()
	if choices[key] != store.Enabled {
		t.Fatalf("cancelled command changed actual choice: %v", choices)
	}
}

func TestRenameSkipsAlreadyCurrentLabel(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Current label", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	if err := f.service.Rename(context.Background(), scope, "one", "Current label"); err != nil {
		t.Fatal(err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "herdr machine rename ") {
			t.Fatalf("unchanged label caused mutation: %s", call)
		}
	}
}
