package registration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSameNameReplacementNeverAuthorizesEntryDeletion(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	item := store.Tracked{SandboxID: "old", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}))
	f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"uuid":"replacement","name":"demo","status":"running"}`))}, nil
	})
	f.rebuildClients()
	result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "old")
	require.Error(t, err)
	require.True(t, result.Retry)
	require.Contains(t, err.Error(), "another identity")
	for _, call := range *calls {
		require.NotContains(t, call, "machine remove", "removed reused target: %s", call)
	}
	state, _ := store.LoadState()
	require.Len(t, state.Scopes[scope], 1)
	require.EqualValues(t, "machine", state.Scopes[scope][0].ProfileID)
}

func TestConfirmedDeletionCleansDisabledEntryOnlyWhileManagementOn(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}}}}))
	_ = store.SetEnabled(scope, "one", store.Disabled)
	f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"code":"sandbox_not_found"}`))}, nil
	})
	f.rebuildClients()
	_ = store.SetWorkerControl(true)
	{
		_, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
		require.Error(t, err, "OFF allowed cleanup")
	}
	require.Len(t, *calls, 0, "OFF issued cleanup commands: %v", *calls)
	_ = store.SetWorkerControl(false)
	{
		_, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
		require.NoError(t, err)
	}
	require.Len(t, *machines, 0, "confirmed deleted Disabled entry retained")
}

func TestResumeRecreatesExternallyRemovedEnabledEntry(t *testing.T) {
	for _, runtime := range []string{"running", "stopped"} {
		t.Run(runtime, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, runtime)
			item := store.Tracked{SandboxID: "one", ProfileID: "removed-machine", Target: "demo.sbx", Label: "Old", ProjectWorkspaceDone: true}
			require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}))
			_ = store.SetEnabled(scope, "one", store.Enabled)
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				call := c.Name + " " + strings.Join(c.Args, " ")
				switch call {
				case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
					return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
				case "herdr machine add --label sbx · demo demo.sbx":
					*calls = append(*calls, call)
					*machines = []herdr.Machine{{ID: "replacement-machine", Label: "sbx · demo", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
					return "Saved SSH machine replacement-machine. Remote server is ready.", nil
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
			var output strings.Builder
			require.NoError(t, f.service.ReconcileContext(context.Background(), &output))
			state, _ := store.LoadState()
			saved := state.Scopes[scope][0]
			if runtime == "running" {
				require.Equal(t, "replacement-machine", saved.ProfileID, "not recreated: %+v\n%s", saved, output.String())
			} else {
				require.EqualValues(t, "", saved.ProfileID)
				require.NotContains(t, output.String(), "cannot add a stopped sandbox")
				for _, call := range *calls {
					require.NotContains(t, call, "machine add")
					require.False(t, strings.HasPrefix(call, "ssh -o"))
				}
			}
		})
	}
}

func TestConfirmedDeletionRetainsCrossScopeAssociation(t *testing.T) {
	for _, sameProfile := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared profile", false: "shared target"}[sameProfile], func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "running")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}
			other := item
			if !sameProfile {
				other.ProfileID = "other-machine"
			}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}, "other-scope": {other}}}
			require.NoError(t, store.SaveState(state))
			f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"code":"sandbox_not_found"}`))}, nil
			})
			f.rebuildClients()
			result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
			require.NoError(t, err)
			require.Contains(t, result.Detail, "retained state")
			for _, call := range *calls {
				require.NotContains(t, call, "machine remove", "ambiguous cross-scope removal: %s", call)
			}
			status, _ := store.ReadStatusStore()
			found := false
			for _, entry := range status.Errors {
				if entry.Scope == store.StatusKey(scope, "one") && entry.Code == store.CodeOwnershipConflict {
					found = true
				}
			}
			require.True(t, found, "association warning was not retained for inspection")
			saved, _ := store.LoadState()
			require.Len(t, saved.Scopes[scope], 1)
			require.Len(t, saved.Scopes["other-scope"], 1)
			require.Len(t, *machines, 1)
		})
	}
}

func TestRemovedEntryDeletionReleasesIdentityForSameNameReplacement(t *testing.T) {
	for _, mode := range []string{"delete event", "missed deletion"} {
		t.Run(mode, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Disabled))
			require.NoError(t, f.service.RemoveEntry(context.Background(), scope, "one"))
			// The old sandbox is gone before cleanup runs; its name is already reused.
			f.inventory = `[{"uuid":"replacement","name":"demo","status":"stopped"}]`
			f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"uuid":"replacement","name":"demo","status":"stopped"}`
				if r.URL.Path == "/sandbox" {
					body = f.inventory
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			f.rebuildClients()
			*machines = []herdr.Machine{{ID: "replacement-entry", Label: "New desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
			*calls = nil
			if mode == "delete event" {
				result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
				require.NoError(t, err)
				require.False(t, result.Retry)
			} else {
				require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
			}
			state, err := store.LoadState()
			require.NoError(t, err)
			for _, item := range state.Scopes[scope] {
				require.NotEqual(t, "one", item.SandboxID, "dead identity retained: %+v", item)
			}
			rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.EqualValues(t, "", rows[0].AssociationIssue)
			require.Len(t, *machines, 1)
			require.EqualValues(t, "replacement-entry", (*machines)[0].ID)
			for _, call := range *calls {
				require.NotContains(t, call, "machine remove", "cleanup removed a replacement entry: %s", call)
			}
		})
	}
}

func TestLocalOnlyDeletionStillRequiresConfirmedIdentityAndManagement(t *testing.T) {
	for _, mode := range []string{"absent", "pending", "unknown", "present", "OFF"} {
		t.Run(mode, func(t *testing.T) {
			f, scope, _, calls := newManagementFixture(t, "stopped")
			item := store.Tracked{SandboxID: "one", Target: "demo.sbx", Pending: mode == "pending", ProjectWorkspaceDone: true}
			require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}))
			f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				status, body := 404, `{"code":"sandbox_not_found"}`
				if mode == "unknown" {
					status, body = 503, `{}`
				}
				if mode == "present" {
					status, body = 200, `{"uuid":"one","name":"demo","status":"stopped"}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			f.rebuildClients()
			if mode == "OFF" {
				require.NoError(t, store.SetWorkerControl(true))
			}
			_, _ = f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
			state, err := store.LoadState()
			require.NoError(t, err)
			require.Equal(t, (mode == "absent"), (len(state.Scopes[scope]) == 0), "mode %s lost/retained unexpected identity: %+v", mode, state)
			require.Len(t, *calls, 0, "local cleanup issued external command: %v", *calls)
		})
	}
}
