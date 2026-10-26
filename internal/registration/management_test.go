package registration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
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
			require.NoError(t, store.SetEnabled("scope", "one", choice))
		}
		rows, err := observeManagement("scope", []sbx.Sandbox{sb}, nil, store.State{})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.False(t, rows[0].Enabled != choice)
		require.False(t, rows[0].EntryPresent)
	}
}

func TestAssociationUsesStableIdentityAndNotLabel(t *testing.T) {
	sb := sbx.Sandbox{ID: "one", Name: "demo"}
	state := store.State{Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "old"}}}}
	m := herdr.Machine{ID: "machine", Label: "external label", Target: "demo.sbx", Session: "default"}
	{
		found, issue := associatedMachine("scope", sb, []herdr.Machine{m}, state)
		require.NotNil(t, found)
		require.EqualValues(t, "", issue)
	}
	for _, other := range []herdr.Machine{{ID: "new", Target: "demo.sbx", Session: "default"}, {ID: "machine", Target: "demo.sbx", Session: "other"}} {
		{
			found, issue := associatedMachine("scope", sb, []herdr.Machine{other}, state)
			require.Nil(t, found)
			require.NotEqualValues(t, "", issue)
		}
	}
	{
		found, issue := associatedMachine("scope", sb, []herdr.Machine{m, m}, state)
		require.Nil(t, found)
		require.NotEqualValues(t, "", issue)
	}
}

func TestStoppedEnableWithoutEntryStaysDisabledQuietly(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "stopped")
	require.NoError(t, store.SetWorkerControl(true))
	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	require.NoError(t, err, "error=%v", err)
	choices, _ := store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Disabled), choices[store.StatusKey(scope, "one")], "unsupported enable was retained")
	for _, call := range *calls {
		require.NotContains(t, call, "machine add")
		require.False(t, strings.HasPrefix(call, "ssh -o"))
		require.NotContains(t, call, "sbx exec")
	}
	status, _ := store.ReadStatusStore()
	require.Len(t, status.Errors, 0, "unexpected diagnostic: %+v", status.Errors)

	row := status.Sandboxes[store.StatusKey(scope, "one")]
	require.EqualValues(t, "stopped", row.Connection)
	require.False(t, row.Enabled != store.Disabled)
}

func TestDisableRemoveEnablePreservesDisabledAndObservedLabel(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.SetWorkerControl(true))
	require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Disabled))
	rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
	require.NoError(t, err)
	require.True(t, rows[0].EntryPresent)
	require.EqualValues(t, "Desk", rows[0].Label)
	require.NotNil(t, rows[0].NativeEnabled)
	require.False(t, *rows[0].NativeEnabled)
	require.NoError(t, f.service.RemoveEntry(context.Background(), scope, "one"))
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	choices, _ := store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Disabled), choices[store.StatusKey(scope, "one")], "removed sandbox rediscovered as enabled")
	{
		err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
		require.NoError(t, err, "unsafe re-enable: %v", err)
	}
}

func TestRemoveFailureKeepsEntryAndRetry(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine remove machine" {
			return "offline", errors.New("offline")
		}
		return execute(ctx, c)
	}
	f.rebuildClients()

	err := f.service.RemoveEntry(context.Background(), scope, "one")
	require.Error(t, err, "hidden failure")

	status, _ := store.ReadStatusStore()
	row := status.Sandboxes[store.StatusKey(scope, "one")]
	require.EqualValues(t, "Desk", row.Label)
	require.True(t, row.EntryPresent)
}

func TestBaselineAcceptsExternalNativeStateAndLabelWhileStopped(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Old", PendingLabel: "cancelled", LastObservedNativeEnabled: boolPointer(true)}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}))
	require.NoError(t, store.SetEnabled(scope, "one", store.Enabled))
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	choices, _ := store.ReadEnabled()
	state, _ := store.LoadState()
	saved := state.Scopes[scope][0]
	require.False(t, choices[store.StatusKey(scope, "one")] != store.Disabled)
	require.EqualValues(t, "External", saved.Label)
	require.EqualValues(t, "", saved.PendingLabel)
	(*machines)[0].Enabled = boolPointer(true)
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	choices, _ = store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Enabled), choices[store.StatusKey(scope, "one")], "external native enable was not accepted")
}

func TestRenameWorksWhileDisabledAndManagementOff(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Old", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	_ = store.SetWorkerControl(true)
	require.NoError(t, f.service.Rename(context.Background(), scope, "one", "New"))
	require.Equal(t, "New", (*machines)[0].Label, "rename not applied")
}

func TestManagementOffRejectsAutomaticWorkBeforeObservation(t *testing.T) {
	f, _, _, calls := newManagementFixture(t, "running")
	require.NoError(t, store.SetWorkerControl(true))

	err := f.service.ReconcileContext(context.Background(), nil)
	require.ErrorIs(t, err, context.Canceled, "automatic work was not cancelled: %v", err)

	require.Len(t, *calls, 0, "OFF issued commands: %v", *calls)
}

func TestUnconfirmedNativeDisableCannotAuthorizeRemoval(t *testing.T) {
	for _, native := range []*bool{nil, boolPointer(true)} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "running")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: native}}
			_ = store.SetEnabled(scope, "one", store.Disabled)

			err := f.service.RemoveEntry(context.Background(), scope, "one")
			require.Error(t, err, "unconfirmed disable accepted")

			for _, call := range *calls {
				require.NotContains(t, call, "machine remove", "unsafe remove: %s", call)
			}
		})
	}
}

func TestCatalogFailurePreservesLastKnownLabel(t *testing.T) {
	f, _, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	{
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		require.NoError(t, err)
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
	require.Error(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, "Desk", rows[0].Label)
	require.True(t, rows[0].EntryPresent)
	require.True(t, rows[0].Status.Stale)
	require.NotEqualValues(t, "", rows[0].AssociationIssue)
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
	require.NoError(t, f.service.Rename(context.Background(), scope, "one", "New"))
	state, _ := store.LoadState()
	require.Equal(t, "New", state.Scopes[scope][0].Label, "actual rename not retained")
}

func TestVerifiedPartialAddRetainsCatalogIdentityOnResume(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Label: "Intent", Pending: true}}}}))
	_ = store.SetEnabled(scope, "one", store.Enabled)
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
	state, _ := store.LoadState()
	item := state.Scopes[scope][0]
	require.EqualValues(t, "machine", item.ProfileID)
	require.EqualValues(t, "demo.sbx", item.Target)
	require.False(t, item.Pending)
	require.EqualValues(t, "Desk", item.Label)
	for _, call := range *calls {
		require.NotContains(t, call, "machine add", "duplicate add: %s", call)
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
			require.NoError(t, err)
			require.False(t, rows[0].Status.Connection != test.want)
			require.NotEqualValues(t, "old ready", rows[0].Status.Detail)
		})
	}
}

func TestDisableDuringMissingEntryEnablePreventsAdd(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "running")
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "ssh" && strings.HasSuffix(strings.Join(c.Args, " "), remoteHerdrStatusShell()) {
			require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
			return remoteHerdrOnPath + "\nherdr 0.9.3", nil
		}
		return execute(ctx, c)
	}
	f.rebuildClients()

	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	require.Error(t, err, "cancelled Enable claimed success")

	for _, call := range *calls {
		require.NotContains(t, call, "machine add", "disabled sandbox added: %s", call)
	}
	choices, _ := store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Disabled), choices[store.StatusKey(scope, "one")], "later Disable lost")
}

func TestDisableDuringFixDownloadPreventsTransferAndHandoff(t *testing.T) {
	f, scope, _, handoffs := newFixHerdrFixture(t, false)
	execute := f.runner.Execute
	transfers := 0
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name != "ssh" {
			return execute(ctx, c)
		}
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
		return execute(ctx, c)
	}
	f.rebuildClients()
	payload := "isolated executable"
	digest := sha256.Sum256([]byte(payload))
	f.service.releaseTransport = releaseTestTransport(func(r *http.Request) (*http.Response, error) {
		return interruptedFixDownload(t, scope, payload, digest, r)
	})
	err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, "/tmp/fix-herdr.sock", "sandbox-1", "demo", "herdr 0.9.3")
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 0, transfers)
	require.EqualValues(t, 0, *handoffs)
}

func TestBaselineCannotOverwriteNewerExplicitDisable(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	_ = store.SetEnabled(scope, "one", store.Enabled)
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if strings.Join(c.Args, " ") == "machine list --json" {
			require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	choices, _ := store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Disabled), choices[store.StatusKey(scope, "one")], "baseline overwrote newer Disable")
	state, _ := store.LoadState()
	require.Equal(t, "External", state.Scopes[scope][0].Label, "independent current label was lost")
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
					require.NoError(t, store.SetEnabled(scope, "one", test.newer))
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			err := f.service.SetEnabled(context.Background(), scope, "one", test.older)
			require.ErrorIs(t, err, context.Canceled, "superseded action=%v", err)
			choices, _ := store.ReadEnabled()
			require.Equal(t, test.newer, choices[store.StatusKey(scope, "one")], "older action overwrote newer choice")
			for _, call := range *calls {
				require.NotContains(t, call, "machine enable")
				require.NotContains(t, call, "machine disable")
				require.NotContains(t, call, "machine add")
				require.False(t, strings.HasPrefix(call, "ssh -o"))
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
	require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
	for _, call := range *calls {
		require.False(t, strings.HasPrefix(call, "ssh -o"), "direct guest connection to stopped sandbox: %s", call)
	}
	require.True(t, *(*machines)[0].Enabled, "explicit Enable did not enable the native connection")
}

func TestSyntheticStatusClearsAfterEnabledRunningObservation(t *testing.T) {
	for _, previous := range []string{"disabled", "waiting for start", "waiting for entry", "disabled in herdr", "disable not confirmed", "workspace setup required"} {
		t.Run(previous, func(t *testing.T) {
			status := store.SandboxStatus{Connection: previous, Detail: "old detail"}
			row := SandboxRow{Enabled: store.Enabled, EntryPresent: true, NativeEnabled: boolPointer(true), Sandbox: sbx.Sandbox{Status: "running"}}
			applyObservedStatus(&status, row)
			if previous == "workspace setup required" {
				require.False(t, status.Connection != previous)
				require.EqualValues(t, "old detail", status.Detail)
			} else {
				require.EqualValues(t, "registered", status.Connection)
				require.NotEqualValues(t, "old detail", status.Detail)
			}
		})
	}
}

func TestUnknownRuntimeIsNeverReportedStopped(t *testing.T) {
	for _, runtime := range []string{"", "unknown", "pending", "failed"} {
		t.Run(runtime, func(t *testing.T) {
			f, scope, _, calls := newManagementFixture(t, runtime)
			var out strings.Builder
			require.NoError(t, f.service.ReconcileContext(context.Background(), &out))
			status, _ := store.ReadStatusStore()
			row := status.Sandboxes[store.StatusKey(scope, "one")]
			require.NotContains(t, strings.ToLower(row.Detail), "stopped")
			require.NotContains(t, out.String(), "cannot add a stopped")
			require.NotEqualValues(t, "ready", row.Connection)
			require.NotEqualValues(t, "waiting for start", row.Connection)

			err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "R0008")
			require.NotContains(t, strings.ToLower(err.Error()), "stopped")

			for _, call := range *calls {
				require.NotContains(t, call, "machine add")
				require.False(t, strings.HasPrefix(call, "ssh -o"))
			}
		})
	}
}

func TestRemoveEnablePreservesCompletedGuestSetup(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	f.inventory = `[{"uuid":"one","name":"demo","status":"running","workspace":"/workspace/demo"}]`
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	fingerprint, err := guestplugins.DesiredFingerprint(context.Background(), f.service.herdr, scope, "one")
	require.NoError(t, err)
	checked := time.Now()
	sync := &store.GuestSyncObservation{Fingerprint: "demo.sbx\x00one\x00" + fingerprint, CheckedAt: checked, Status: "ready", Detail: "already registered"}
	item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk", ProjectWorkspaceDone: true, GuestSync: sync}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}))
	_ = store.SetEnabled(scope, "one", store.Disabled)
	_ = store.RecordGuestServerObservation(scope, sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}, true, "0.9.3")
	require.NoError(t, f.service.RemoveEntry(context.Background(), scope, "one"))
	removed, _ := store.LoadState()
	kept := removed.Scopes[scope][0]
	require.EqualValues(t, "", kept.ProfileID)
	require.True(t, kept.ProjectWorkspaceDone)
	require.NotNil(t, kept.GuestSync)
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
	require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
	state, _ := store.LoadState()
	restored := state.Scopes[scope][0]
	require.True(t, restored.ProjectWorkspaceDone)
	require.NotNil(t, restored.GuestSync)
	require.True(t, restored.GuestSync.CheckedAt.Equal(checked))
	for _, call := range *calls {
		require.NotContains(t, call, "pane list")
		require.NotContains(t, call, "workspace create")
		require.NotContains(t, call, "server start")
		require.NotContains(t, call, "status server --json")
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
			require.NoError(t, err)
			require.EqualValues(t, "registered", rows[0].Status.Connection)
			require.EqualValues(t, "", rows[0].AssociationIssue)
			status, _ := store.ReadStatusStore()
			require.Len(t, status.Errors, 0, "stale conflict: %+v", status.Errors)
			rows, err = observeManagement("scope", []sbx.Sandbox{sb}, append(machines, machines[0]), store.State{})
			require.NoError(t, err)
			require.EqualValues(t, "More than one herdr entry uses this sandbox target.", rows[0].AssociationIssue)
			rows, err = observeManagement("scope", []sbx.Sandbox{sb}, nil, store.State{})
			require.NoError(t, err)
			require.EqualValues(t, "not registered", rows[0].Status.Connection)
			require.EqualValues(t, "", rows[0].AssociationIssue)
		})
	}
}

func TestStoppedDiscoveryDoesNotEnableOnLaterStart(t *testing.T) {
	f, scope, _, calls := newManagementFixture(t, "stopped")
	_ = store.SetRegistrationError(store.CodeSafeEntryUnsupported, store.StatusKey(scope, "one"), "old unsupported entry")
	for _, runtime := range []string{"stopped", "stopped", "running"} {
		f.inventory = fmt.Sprintf(`[{"uuid":"one","name":"demo","status":%q}]`, runtime)
		var out strings.Builder
		require.NoError(t, f.service.ReconcileContext(context.Background(), &out))
		status, _ := store.ReadStatusStore()
		row := status.Sandboxes[store.StatusKey(scope, "one")]
		require.False(t, row.Enabled != store.Disabled)
		require.Len(t, status.Errors, 0)
		require.False(t, runtime == "stopped" && row.Connection != "stopped", "row=%+v", row)
	}
	for _, call := range *calls {
		require.NotContains(t, call, "machine add")
		require.False(t, strings.HasPrefix(call, "ssh -o"))
		require.NotContains(t, call, "sbx exec")
	}
}

func TestObservationRetainsCurrentCreationLabelCollision(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sb := sbx.Sandbox{ID: "one", Name: "demo", Status: "running"}
	key := store.StatusKey("scope", "one")
	detail := "Another herdr entry already uses this label. Rename that entry before creating one for this sandbox."
	require.NoError(t, store.RecordSandboxStatus("scope", sb, "association warning", detail, false))
	require.NoError(t, store.SetRegistrationError(store.CodeOwnershipConflict, key, detail))
	machines := []herdr.Machine{{ID: "other", Label: sandboxMachineLabel(sb.Name), Target: "other.sbx", Session: "default", Enabled: boolPointer(true)}}
	rows, err := observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	require.NoError(t, err)
	require.EqualValues(t, "label already in use", rows[0].Status.Connection)
	require.False(t, rows[0].EntryPresent)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1)
	machines = append(machines, herdr.Machine{ID: "machine", Label: "external label", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)})
	rows, err = observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	require.NoError(t, err)
	require.EqualValues(t, "registered", rows[0].Status.Connection)
	require.True(t, rows[0].EntryPresent)
	machines = machines[:1]
	machines[0].Label = "renamed"
	rows, err = observeManagement("scope", []sbx.Sandbox{sb}, machines, store.State{})
	require.NoError(t, err)
	require.EqualValues(t, "not registered", rows[0].Status.Connection)
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 0)
}

func TestExplicitEnableStoppedExistingEntryEnablesNativeConnection(t *testing.T) {
	for _, managementOff := range []bool{false, true} {
		t.Run(fmt.Sprintf("management_off_%t", managementOff), func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			require.NoError(t, store.SetWorkerControl(managementOff))
			require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
			require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
			require.True(t, *(*machines)[0].Enabled, "explicit Enable left the native herdr entry disabled")
			require.NoError(t, store.SetWorkerControl(false))
			var output strings.Builder
			require.NoError(t, f.service.ReconcileContext(context.Background(), &output))
			require.True(t, *(*machines)[0].Enabled, "reconciliation undid explicit Enable while sandbox stayed stopped")
			for _, call := range *calls {
				require.NotContains(t, call, "machine disable")
				require.NotContains(t, call, "machine add")
				require.False(t, strings.HasPrefix(call, "ssh -o"))
				require.NotContains(t, call, "sbx exec")
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
			require.NoError(t, store.SetEnabled(scope, "one", test.choice))
			require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
			require.Equal(t, test.want, *(*machines)[0].Enabled, "native enabled=%t, want %t", *(*machines)[0].Enabled, test.want)
			for _, call := range *calls {
				require.NotContains(t, call, "machine enable")
				require.NotContains(t, call, "machine add")
				require.False(t, strings.HasPrefix(call, "ssh -o"))
				require.NotContains(t, call, "sbx exec")
				require.False(t, test.choice == store.Enabled && strings.Contains(call, "machine disable"), "Enabled choice was disabled: %s", call)
			}
		})
	}
}

func TestStoppedExplicitEnableVerifiesNativeOutcome(t *testing.T) {
	for _, test := range []stoppedEnableCase{
		{"rejected", false, true, false},
		{"unconfirmed success", false, false, false},
		{"unknown native state", false, false, true},
		{"lost acknowledgement", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			testStoppedExplicitEnableOutcome(t, test)
		})
	}
}

func TestNativeToggleSettlesToObservedChoice(t *testing.T) {
	for _, test := range []nativeToggleCase{
		{"rejected enable", false, true, false},
		{"rejected disable", true, false, false},
		{"applied enable with lost acknowledgement", false, true, true},
		{"applied disable with lost acknowledgement", true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testNativeToggleOutcome(t, test)
		})
	}
}

func TestUnverifiedNativeToggleWaitsForObservationWithoutReplay(t *testing.T) {
	for _, catalogUnavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("catalog_unavailable_%t", catalogUnavailable), func(t *testing.T) {
			testUnverifiedNativeToggle(t, catalogUnavailable)
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
	{
		err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
		require.Error(t, err, "unverified mutation reported success")
	}
	close(release)
	require.NoError(t, <-finished)
	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	require.True(t, status.EnabledObservationPending[key])
	require.False(t, status.EnabledChoices[key] != store.Disabled)
	require.True(t, status.Sandboxes[key].Stale)
	mutated = false
	{
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		require.NoError(t, err)
	}
	status, _ = store.ReadStatusStore()
	require.False(t, status.EnabledObservationPending[key])
	require.False(t, status.EnabledChoices[key] != store.Enabled)
}

func TestExplicitAddRequiresVerifiedEnabledEntry(t *testing.T) {
	for _, outcome := range []string{"absent", "disabled", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			testExplicitAddOutcome(t, outcome)
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
	require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
	close(release)
	require.NoError(t, <-finished)
	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	require.NotNil(t, status.Sandboxes[key].NativeEnabled)
	require.True(t, *status.Sandboxes[key].NativeEnabled)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	{
		err := f.service.SetEnabled(ctx, scope, "one", store.Disabled)
		require.ErrorIs(t, err, context.Canceled, "cancellation=%v", err)
	}
	choices, _ := store.ReadEnabled()
	require.Equal(t, store.EnabledChoice(store.Enabled), choices[key], "cancelled command changed actual choice: %v", choices)
}

func TestRenameSkipsAlreadyCurrentLabel(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "Current label", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	require.NoError(t, f.service.Rename(context.Background(), scope, "one", "Current label"))
	for _, call := range *calls {
		require.False(t, strings.HasPrefix(call, "herdr machine rename "), "unchanged label caused mutation: %s", call)
	}
}

type stoppedEnableCase struct {
	name                     string
	applied, failed, unknown bool
}

func testStoppedExplicitEnableOutcome(t *testing.T, test stoppedEnableCase) {
	t.Helper()
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	execute := f.runner.Execute
	f.runner.Execute = stoppedEnableExecutor(test, machines, execute)
	f.rebuildClients()
	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	require.Equal(t, test.applied, (err == nil), "applied=%t error=%v", test.applied, err)
	rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
	require.NoError(t, err)
	require.False(t, (rows[0].NativeEnabled == nil) != test.unknown)
	require.False(t, rows[0].NativeEnabled != nil && *rows[0].NativeEnabled != test.applied)
	require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	want := "disabled"
	if test.applied {
		want = "waiting for start"
	} else if test.unknown {
		want = "observation unavailable"
	}
	require.False(t, rows[0].Status.Connection != want)
	require.False(t, status.Sandboxes[store.StatusKey(scope, "one")].Connection != want)
}

type nativeToggleCase struct {
	name                        string
	initial, requested, applied bool
}

func testNativeToggleOutcome(t *testing.T, test nativeToggleCase) {
	t.Helper()
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
	f.runner.Execute = nativeToggleExecutor(test, machines, execute)
	f.rebuildClients()
	err := f.service.SetEnabled(context.Background(), scope, "one", requested)
	require.Equal(t, test.applied, (err == nil), "applied=%t err=%v", test.applied, err)
	want := oldChoice
	if test.applied {
		want = requested
	}
	choices, _ := store.ReadEnabled()
	require.Equal(t, want, choices[store.StatusKey(scope, "one")], "requested choice retained: choices=%v want=%s", choices, want)
}

func testUnverifiedNativeToggle(t *testing.T, catalogUnavailable bool) {
	t.Helper()
	f, scope, machines, calls := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Label: "External", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	{
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		require.NoError(t, err)
	}
	execute := f.runner.Execute
	f.runner.Execute = unverifiedToggleExecutor(&catalogUnavailable, machines, calls, execute)
	f.rebuildClients()
	{
		err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
		require.Error(t, err, "unverified change reported success")
	}
	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	require.False(t, status.EnabledChoices[key] != store.Disabled)
	require.True(t, status.Sandboxes[key].Stale)
	_ = f.service.ReconcileContext(context.Background(), io.Discard)
	enables := 0
	for _, call := range *calls {
		if call == "herdr machine enable machine" {
			enables++
		}
	}
	require.Equal(t, 1, enables, "unverified action replayed: %d", enables)
	catalogUnavailable = false
	(*machines)[0].Enabled = boolPointer(true)
	{
		_, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
		require.NoError(t, err)
	}
	status, _ = store.ReadStatusStore()
	require.False(t, status.EnabledChoices[key] != store.Enabled)
	require.False(t, status.Sandboxes[key].Stale)
}

func testExplicitAddOutcome(t *testing.T, outcome string) {
	t.Helper()
	f, scope, machines, _ := newManagementFixture(t, "running")
	f.runner.Execute = explicitAddExecutor(outcome, machines, f.runner.Execute)
	f.rebuildClients()

	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	require.Error(t, err, "unverified or disabled entry reported successful Enable")

	status, _ := store.ReadStatusStore()
	key := store.StatusKey(scope, "one")
	require.Equal(t, store.EnabledChoice(store.Disabled), status.EnabledChoices[key], "requested choice retained: %+v", status.Sandboxes[key])
	require.Equal(t, (outcome == "unknown"), status.EnabledObservationPending[key], "wrong observation wait: %+v", status.Sandboxes[key])
	require.False(t, outcome == "unknown" && (!status.Sandboxes[key].EntryPresent || status.Sandboxes[key].Label == ""), "actual added entry lost: %+v", status.Sandboxes[key])
}

func stoppedEnableExecutor(test stoppedEnableCase, machines *[]herdr.Machine, execute func(context.Context, process.Command) (string, error)) func(context.Context, process.Command) (string, error) {
	return func(ctx context.Context, c process.Command) (string, error) {
		if c.Name != "herdr" || strings.Join(c.Args, " ") != "machine enable machine" {
			return execute(ctx, c)
		}
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
}

func nativeToggleExecutor(test nativeToggleCase, machines *[]herdr.Machine, execute func(context.Context, process.Command) (string, error)) func(context.Context, process.Command) (string, error) {
	return func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "herdr" && (strings.Join(c.Args, " ") == "machine enable machine" || strings.Join(c.Args, " ") == "machine disable machine") {
			if test.applied {
				(*machines)[0].Enabled = boolPointer(test.requested)
			}
			return "request failed", errors.New("request failed")
		}
		return execute(ctx, c)
	}
}

func unverifiedToggleExecutor(catalogUnavailable *bool, machines *[]herdr.Machine, calls *[]string, execute func(context.Context, process.Command) (string, error)) func(context.Context, process.Command) (string, error) {
	mutated := false
	return func(ctx context.Context, c process.Command) (string, error) {
		switch c.Name + " " + strings.Join(c.Args, " ") {
		case "herdr machine enable machine":
			*calls = append(*calls, "herdr machine enable machine")
			mutated = true
			(*machines)[0].Enabled = nil
			return "lost response", errors.New("lost response")
		case "herdr machine list --json":
			if mutated && *catalogUnavailable {
				return "catalog unavailable", errors.New("catalog unavailable")
			}
		}
		return execute(ctx, c)
	}
}

func explicitAddExecutor(outcome string, machines *[]herdr.Machine, execute func(context.Context, process.Command) (string, error)) func(context.Context, process.Command) (string, error) {
	added, postAddReads := false, 0
	return func(ctx context.Context, c process.Command) (string, error) {
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
					applyExplicitAddOutcome(outcome, machines)
				}
			}
		}
		return execute(ctx, c)
	}
}

func applyExplicitAddOutcome(outcome string, machines *[]herdr.Machine) {
	switch outcome {
	case "absent":
		*machines = []herdr.Machine{}
	case "disabled":
		(*machines)[0].Enabled = boolPointer(false)
	case "unknown":
		(*machines)[0].Enabled = nil
	}
}

func interruptedFixDownload(t *testing.T, scope, payload string, digest [32]byte, r *http.Request) (*http.Response, error) {
	t.Helper()
	body := payload
	if r.URL.String() == releaseManifestURL {
		raw, _ := json.Marshal(herdrRelease{Version: "0.9.3", Assets: map[string]string{"linux-x86_64": "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64"}, SHA256: map[string]string{"linux-x86_64": hex.EncodeToString(digest[:])}})
		body = string(raw)
	} else {
		require.NoError(t, store.SetEnabled(scope, "sandbox-1", store.Disabled))
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
