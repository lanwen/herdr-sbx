package registration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGuestShellSetupUsesBoundedReadinessAudit(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "status server --json") {
			return `{"running":true,"version":"0.9.3"}`, nil
		}
		return "", errors.New("unexpected command")
	}
	f.rebuildClients()
	item := store.Tracked{SandboxID: "id", ProfileID: "machine", Target: "demo.sbx", ProjectWorkspaceDone: true}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	work := registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}, Current: func() bool { return true }}
	for range 3 {
		require.Equal(t, "ready", f.service.projectReadiness(context.Background(), work, &item, true).Status)
	}
	require.Equal(t, 1, f.shellSetups)
	item.GuestSync.CheckedAt = time.Now().Add(-16 * time.Minute)
	require.Equal(t, "ready", f.service.projectReadiness(context.Background(), work, &item, true).Status)
	require.Equal(t, 2, f.shellSetups)
	f.shellSetupErr = process.ErrTimeout
	outcome := f.service.projectReadiness(WithFreshGuestObservation(context.Background()), work, &item, true)
	require.Equal(t, "preparation timed out", outcome.Status)
	require.Equal(t, store.CodeGuestShellTimeout, resultDiagnosticCode(outcome))
	require.Contains(t, outcome.Detail, "sandbox preparation timed out when trying to update Bash startup files")
	require.Equal(t, "machine", item.ProfileID)
	cached := f.service.projectReadiness(context.Background(), work, &item, true)
	require.Equal(t, "preparation timed out", cached.Status)
	require.Equal(t, store.CodeGuestShellTimeout, resultDiagnosticCode(cached))
}

func TestGuestShellTimeoutDiagnosticClearsAfterRecovery(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	f.runner.Execute = func(context.Context, process.Command) (string, error) {
		return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
	}
	f.shellSetupErr = process.ErrTimeout
	f.rebuildClients()
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
	work := registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}, Current: func() bool { return true }}
	outcome, saved := f.service.registerPreparedContext(context.Background(), work, nil)
	require.Nil(t, saved)
	require.Equal(t, "preparation timed out", outcome.Status)
	require.Equal(t, store.CodeGuestShellTimeout, resultDiagnosticCode(outcome))
	scope := store.StatusKey("scope", "id")
	require.NoError(t, saveResultError(scope, outcome))
	primary, found, err := store.PrimaryError(scope)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, store.CodeGuestShellTimeout, primary.Code)
	require.Contains(t, registrationNotification(outcome, primary), "Sandbox preparation timed out")
	require.NoError(t, saveResultError(scope, result{Status: "ready"}))
	_, found, err = store.PrimaryError(scope)
	require.NoError(t, err)
	require.False(t, found)
}

func TestInitialRegistrationReportsGuestConnectionFailure(t *testing.T) {
	for _, stage := range []string{"guest inspection", "shell setup"} {
		t.Run(stage, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			f.runner.Execute = func(context.Context, process.Command) (string, error) {
				if stage == "guest inspection" {
					return "", process.ErrTimeout
				}
				return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
			}
			f.shellSetupErr = errors.New("timed out")
			f.rebuildClients()
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
			outcome, saved := f.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}, Current: func() bool { return true }}, nil)
			want := "connection failed"
			if stage == "shell setup" {
				want = "preparation failed"
			}
			require.Equal(t, want, outcome.Status)
			require.Equal(t, store.CodeSandboxSetup, resultDiagnosticCode(outcome))
			require.Contains(t, outcome.Detail, "timed out")
			require.Nil(t, saved)
		})
	}
}

func TestGuestShellSetupChecksEligibility(t *testing.T) {
	for scenario, runtime := range map[string]string{"management off": "running", "stopped": "stopped", "unknown": "", "stale": "running"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			sandbox := sbx.Sandbox{ID: "id", Name: "demo", Status: runtime}
			current := func() bool { return scenario != "stale" }
			if scenario == "management off" {
				require.NoError(t, store.UpdateStatusStore(func(status *store.StatusStore) { status.WorkerControl = &store.WorkerControl{Stopped: true} }))
			}
			require.Error(t, f.service.prepareGuestShell(context.Background(), sandbox, current))
			require.Zero(t, f.shellSetups)
		})
	}
}
