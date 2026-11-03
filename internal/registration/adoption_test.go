package registration

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStoppedExplicitEnableAddsProjectConnection(t *testing.T) {
	for _, off := range []bool{false, true} {
		t.Run(map[bool]string{false: "ON", true: "OFF"}[off], func(t *testing.T) {
			f := newProjectReaddFixture(t, "fresh server")
			f.profile = ""
			f.commands.sandbox.Status = "stopped"
			setFixtureInventory(f.fixture, f.commands.sandbox)
			require.NoError(t, store.SetWorkerControl(off))
			require.NoError(t, f.fixture.service.SetEnabled(context.Background(), f.scope, f.commands.sandbox.ID, store.Enabled))
			f.assertProjectRestored()
			require.Equal(t, 1, f.commands.adds)
		})
	}
}

func TestAutomaticAdoptionSettlesNativeState(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "manual", Target: "demo.sbx", Label: "My project", Session: "default", Enabled: boolPointer(enabled)}}
			require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
			require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Pending: true, ProjectWorkspaceDone: true}}}}))
			require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
			state, err := store.LoadState()
			require.NoError(t, err)
			require.Equal(t, "manual", state.Scopes[scope][0].ProfileID)
			require.False(t, state.Scopes[scope][0].Pending)
			require.False(t, state.Scopes[scope][0].ProjectWorkspaceDone)
			status, err := store.ReadStatusStore()
			require.NoError(t, err)
			want := store.Disabled
			if enabled {
				want = store.Enabled
			}
			require.Equal(t, want, status.EnabledChoices[store.StatusKey(scope, "one")])
			for _, call := range *calls {
				require.NotContains(t, call, "machine add")
				require.NotContains(t, call, "machine enable")
				require.NotContains(t, call, "ssh -o")
			}
		})
	}
}

func TestExplicitEnableAdoptsUnacknowledgedConnectionWhileOFF(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Pending: true}}}}))
	require.NoError(t, store.SetWorkerControl(true))
	require.ErrorIs(t, f.service.ReconcileContext(context.Background(), io.Discard), context.Canceled)
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Empty(t, state.Scopes[scope][0].ProfileID)
	require.NoError(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
	state, err = store.LoadState()
	require.NoError(t, err)
	require.Equal(t, "machine", state.Scopes[scope][0].ProfileID)
}

func TestPendingAdoptionRejectsConflictingCandidates(t *testing.T) {
	for _, test := range []struct {
		name     string
		machines []herdr.Machine
		ack      string
		owner    bool
	}{
		{name: "wrong session", machines: []herdr.Machine{{ID: "manual", Target: "demo.sbx", Session: "other"}}},
		{name: "multiple", machines: []herdr.Machine{{ID: "one", Target: "demo.sbx", Session: "default"}, {ID: "two", Target: "demo.sbx", Session: "default"}}},
		{name: "ack conflict", machines: []herdr.Machine{{ID: "manual", Target: "demo.sbx", Session: "default"}}, ack: "trusted"},
		{name: "other owner", machines: []herdr.Machine{{ID: "manual", Target: "demo.sbx", Session: "default"}}, owner: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := store.State{Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "one", Target: "demo.sbx", Pending: true, PendingProfileID: test.ack}}}}
			if test.owner {
				state.Scopes["other"] = []store.Tracked{{SandboxID: "other", ProfileID: "manual"}}
			}
			machine, issue := associatedMachine("scope", sbx.Sandbox{ID: "one", Name: "demo"}, test.machines, state)
			require.Nil(t, machine)
			require.NotEmpty(t, issue)
		})
	}
}

func TestAdoptionRejectsChangedIdentityOrUnreadableCatalog(t *testing.T) {
	for _, mode := range []string{"replacement", "unreadable", "new Disable"} {
		t.Run(mode, func(t *testing.T) { testRejectedAdoption(t, mode) })
	}
}

func testRejectedAdoption(t *testing.T, mode string) {
	t.Helper()
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "manual", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Pending: true, ProjectWorkspaceDone: true}}}}))
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
		if c.Name == "herdr" && strings.Join(c.Args, " ") == "machine list --json" {
			switch mode {
			case "replacement":
				setFixtureInventory(f, sbx.Sandbox{ID: "replacement", Name: "demo", Status: "stopped"})
			case "unreadable":
				return "unavailable", errors.New("unavailable")
			case "new Disable":
				require.NoError(t, store.SetEnabled(scope, "one", store.Disabled))
			}
		}
		return execute(ctx, c)
	}
	f.rebuildClients()
	err := f.service.ReconcileContext(context.Background(), io.Discard)
	if mode != "new Disable" {
		require.Error(t, err)
	}
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Empty(t, state.Scopes[scope][0].ProfileID)
}

func TestExplicitEnableRetriesPendingWithoutCandidate(t *testing.T) {
	f := newProjectReaddFixture(t, "fresh server")
	f.profile = ""
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: {{SandboxID: f.commands.sandbox.ID, Target: "demo.sbx", Pending: true}}}}))
	require.NoError(t, f.fixture.service.SetEnabled(context.Background(), f.scope, f.commands.sandbox.ID, store.Enabled))
	f.assertProjectRestored()
}

func TestPendingLabelOnlyMatchCannotAuthorizeAdoption(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "unrelated", Target: "other.sbx", Label: "sbx · demo", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Pending: true}}}}))
	require.Error(t, f.service.SetEnabled(context.Background(), scope, "one", store.Enabled))
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Empty(t, state.Scopes[scope][0].ProfileID)
	for _, call := range *calls {
		require.NotContains(t, call, "machine add")
		require.NotContains(t, call, "machine enable")
	}
}

func TestBaselineDoesNotAdoptNewAssociationWhileOFF(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "manual", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", Pending: true}}}}))
	require.NoError(t, store.SetWorkerControl(true))
	require.NoError(t, f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"))
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Empty(t, state.Scopes[scope][0].ProfileID)
	require.True(t, state.Scopes[scope][0].Pending)
}
