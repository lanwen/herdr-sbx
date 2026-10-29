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
	f.shellSetupErr = errors.New("permission denied")
	outcome := f.service.projectReadiness(WithFreshGuestObservation(context.Background()), work, &item, true)
	require.Equal(t, "setup required", outcome.Status)
	require.Equal(t, store.CodeSandboxSetup, resultDiagnosticCode(outcome))
	require.Contains(t, outcome.Detail, "permission denied")
	require.Equal(t, "machine", item.ProfileID)
}

func TestGuestShellSetupChecksEligibility(t *testing.T) {
	for _, scenario := range []string{"management off", "stopped", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			sandbox := sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}
			current := func() bool { return scenario != "stale" }
			if scenario == "management off" {
				require.NoError(t, store.UpdateStatusStore(func(status *store.StatusStore) { status.WorkerControl = &store.WorkerControl{Stopped: true} }))
			}
			if scenario == "stopped" {
				sandbox.Status = "stopped"
			}
			require.Error(t, f.service.prepareGuestShell(context.Background(), sandbox, current))
			require.Zero(t, f.shellSetups)
		})
	}
}
