package registration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestVerifiedConnectionResolvesOnlyItsAbsentAddFailure(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "old sandbox scope", true: "qualified scope"}[scoped], func(t *testing.T) {
			testRecoveredAddFailure(t, scoped)
		})
	}
}

func testRecoveredAddFailure(t *testing.T, scoped bool) {
	t.Helper()
	f, scope, machines, detail := newFailedExplicitAddFixture(t)
	errorScope := "one"
	if scoped {
		errorScope = store.StatusKey(scope, "one")
	}
	failed := recordAbsentAddDiagnostic(t, errorScope, detail)
	for _, other := range []string{"rename failed", "remove failed", "disable failed"} {
		require.NoError(t, store.RecordError(store.CodeManagement, errorScope, other))
	}
	require.NoError(t, store.RecordError(store.CodeManagement, "another sandbox", detail))
	*machines = []herdr.Machine{{ID: "manual", Target: "demo.sbx", Label: "Project", Session: "default", Enabled: boolPointer(true)}}
	observeRecoveredCatalog(f, machines)
	setFixtureInventory(f, sbx.Sandbox{ID: "one", Name: "demo", Status: "stopped"})
	require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Equal(t, "manual", state.Scopes[scope][0].ProfileID, "connection was not adopted")
	assertRecoveredAddDiagnostic(t, failed, 4)
	require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
	assertRecoveredAddDiagnostic(t, failed, 4)
}

func newFailedExplicitAddFixture(t *testing.T) (*registrationFixture, string, *[]herdr.Machine, string) {
	t.Helper()
	f, scope, machines, _ := newManagementFixture(t, "running")
	execute := explicitAddExecutor("absent", machines, f.runner.Execute)
	f.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
		output, err := execute(ctx, command)
		if command.Name == "herdr" && strings.HasPrefix(strings.Join(command.Args, " "), "machine add ") {
			return "Herdr could not save the connection", errors.New("connection lost")
		}
		return output, err
	}
	f.rebuildClients()
	err := f.service.SetEnabled(context.Background(), scope, "one", store.Enabled)
	require.ErrorContains(t, err, "the herdr entry is absent after setup")
	return f, scope, machines, err.Error()
}

func observeRecoveredCatalog(f *registrationFixture, machines *[]herdr.Machine) {
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.Join(command.Args, " ") == "machine list --json" {
			data, err := json.Marshal(*machines)
			return string(data), err
		}
		return execute(ctx, command)
	}
	f.rebuildClients()
}

func recordAbsentAddDiagnostic(t *testing.T, scope, detail string) store.ErrorRecord {
	t.Helper()
	require.NoError(t, store.RecordError(store.CodeManagement, scope, detail))
	var failed store.ErrorRecord
	require.NoError(t, store.UpdateStatusStore(func(status *store.StatusStore) {
		for key, entry := range status.Errors {
			if entry.Scope == scope && entry.Detail == detail {
				entry.Output, entry.Command, entry.Recovery = "setup evidence", "Enable", "retry setup"
				entry.LastSeen = time.Now().Add(-2 * time.Hour)
				status.Errors[key] = entry
				failed = entry
			}
		}
	}))
	require.NotEmpty(t, failed.Ref)
	return failed
}

func assertRecoveredAddDiagnostic(t *testing.T, failed store.ErrorRecord, remaining int) {
	t.Helper()
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, remaining, "resolved add failure is still active or unrelated failures were cleared")
	var recovered []store.EventRecord
	for _, event := range status.Events {
		if event.Ref == failed.Ref && event.Resolved {
			recovered = append(recovered, event)
		}
	}
	require.Len(t, recovered, 1, "recovery missing or duplicated")
	event := recovered[0]
	require.Equal(t, failed.Detail, event.Detail)
	require.Equal(t, failed.Code, event.Code)
	require.Equal(t, failed.Scope, event.Scope)
	require.Equal(t, failed.Output, event.Output)
	require.Equal(t, failed.Command, event.Command)
	require.Equal(t, failed.Recovery, event.Recovery)
	require.WithinDuration(t, time.Now(), event.At, time.Minute)
}

func TestConfirmedDisabledConnectionResolvesOldAbsentAddFailure(t *testing.T) {
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", Target: "demo.sbx", ProfileID: "machine"}}}}))
	failed := recordAbsentAddDiagnostic(t, "one", "previous setup failed\nthe herdr entry is absent after setup")
	require.NoError(t, f.service.ReconcileContext(context.Background(), io.Discard))
	assertRecoveredAddDiagnostic(t, failed, 0)
}

func TestUnverifiedConnectionKeepsAbsentAddFailure(t *testing.T) {
	for _, mode := range []string{"multiple", "unreadable", "replaced"} {
		t.Run(mode, func(t *testing.T) { testUnverifiedAddDiagnostic(t, mode) })
	}
}

func testUnverifiedAddDiagnostic(t *testing.T, mode string) {
	t.Helper()
	f, _, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	failed := recordAbsentAddDiagnostic(t, "one", "previous setup failed\nthe herdr entry is absent after setup")
	if mode == "multiple" {
		*machines = append(*machines, (*machines)[0])
	}
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.Join(command.Args, " ") == "machine list --json" {
			if mode == "unreadable" {
				return "unreadable", errors.New("unreadable")
			}
			if mode == "replaced" {
				setFixtureInventory(f, sbx.Sandbox{ID: "replacement", Name: "demo", Status: "stopped"})
			}
		}
		return execute(ctx, command)
	}
	f.rebuildClients()
	_ = f.service.ReconcileContext(context.Background(), io.Discard)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	found := false
	for _, entry := range status.Errors {
		found = found || entry.Ref == failed.Ref
	}
	require.True(t, found, "unverified connection resolved its failure")
}
