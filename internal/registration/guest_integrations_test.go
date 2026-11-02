package registration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGuestIntegrationsUsePreparationCacheAndRecovery(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if strings.Contains(strings.Join(command.Args, " "), "status server --json") {
			return `{"running":true,"version":"0.9.3"}`, nil
		}
		return "", errors.New("unexpected command")
	}
	f.rebuildClients()
	item := store.Tracked{SandboxID: "id", ProfileID: "machine", Target: "demo.sbx", ProjectWorkspaceDone: true}
	item.GuestSync = &store.GuestSyncObservation{Fingerprint: "shell-agent-v1\x00agent-integrations-v1\x00demo.sbx\x00id\x00", CheckedAt: time.Now(), Status: "ready"}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	work := registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}, Current: func() bool { return true }}
	require.Equal(t, "ready", f.service.projectReadiness(context.Background(), work, &item, false).Status)
	require.Equal(t, "ready", f.service.projectReadiness(context.Background(), work, &item, true).Status)
	require.Equal(t, 1, f.integrationSetups)
	item.GuestSync.CheckedAt = time.Now().Add(-16 * time.Minute)
	f.integrationSetupErr = errors.New("integration configuration is not writable")
	outcome := f.service.projectReadiness(context.Background(), work, &item, true)
	require.Equal(t, "preparation failed", outcome.Status)
	require.Equal(t, store.CodeSandboxSetup, resultDiagnosticCode(outcome))
	require.Contains(t, outcome.Detail, "integration configuration is not writable")
	require.Equal(t, "machine", state.Scopes["scope"][0].ProfileID)
	require.Equal(t, "preparation failed", f.service.projectReadiness(context.Background(), work, &item, true).Status)
	require.Equal(t, 2, f.integrationSetups)
	f.integrationSetupErr = nil
	require.Equal(t, "ready", f.service.projectReadiness(WithFreshGuestObservation(context.Background()), work, &item, true).Status)
	require.Equal(t, 3, f.integrationSetups)
}

func TestGuestIntegrationsCheckEligibility(t *testing.T) {
	for _, scenario := range []string{"management off", "stopped", "unknown", "stale", "unverified"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			sandbox := sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}
			current := func() bool { return scenario != "stale" }
			switch scenario {
			case "management off":
				require.NoError(t, store.UpdateStatusStore(func(status *store.StatusStore) { status.WorkerControl = &store.WorkerControl{Stopped: true} }))
			case "stopped", "unknown":
				sandbox.Status = scenario
			case "unverified":
				current = nil
			}
			require.Error(t, f.service.prepareGuestIntegrations(context.Background(), sandbox, "demo.sbx", current))
			require.Zero(t, f.integrationSetups)
		})
	}
}

func TestGuestIntegrationsRecheckEligibilityAfterDiscovery(t *testing.T) {
	for _, scenario := range []string{"management off", "stale identity", "cancelled"} {
		t.Run(scenario, func(t *testing.T) { testGuestIntegrationEligibilityChange(t, scenario) })
	}
}

func testGuestIntegrationEligibilityChange(t *testing.T, scenario string) {
	t.Helper()
	f := newRegistrationFixture(t, "[]")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	current := true
	installs := 0
	f.service.herdr = herdr.New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		if !strings.Contains(strings.Join(command.Args, " "), "api schema --json") {
			if strings.Contains(strings.Join(command.Args, " "), `integration install "$target"`) {
				installs++
			}
			return `{"running":true,"version":"0.9.3","result":{"plugins":[]}}`, nil
		}
		switch scenario {
		case "management off":
			require.NoError(t, store.UpdateStatusStore(func(status *store.StatusStore) { status.WorkerControl = &store.WorkerControl{Stopped: true} }))
		case "stale identity":
			current = false
		case "cancelled":
			cancel()
		}
		return `{"schemas":{"request":{"$defs":{"IntegrationTarget":{"enum":["codex"]}}}}}`, nil
	}})
	previous := &store.GuestSyncObservation{Fingerprint: "previous", Status: "ready", CheckedAt: time.Now()}
	item := store.Tracked{SandboxID: "id", ProfileID: "machine", Target: "demo.sbx", ProjectWorkspaceDone: true, GuestSync: previous}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	work := registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}, Current: func() bool { return current }}
	outcome := f.service.projectReadiness(ctx, work, &item, true)
	require.Equal(t, "stale", outcome.Status)
	require.Same(t, previous, item.GuestSync)
	require.Same(t, previous, state.Scopes["scope"][0].GuestSync)
	require.Zero(t, installs)
}
