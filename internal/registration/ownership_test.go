package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRegistrationIdentityChangesKeepSafeJournalState(t *testing.T) {
	t.Run("changed before add", func(t *testing.T) {
		fixture := newRegistrationFixture(t, "[]")
		adds, checks := 0, 0
		fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
			call := command.Name + " " + strings.Join(command.Args, " ")
			switch call {
			case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
				return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
			case "herdr machine add --label sbx · demo demo.sbx":
				adds++
				return "Saved SSH machine machine-1. Remote server is ready.", nil
			default:
				return "", fmt.Errorf("unexpected call before stale check: %s", call)
			}
		}
		fixture.rebuildClients()
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, saved := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, Current: func() bool {
			checks++
			return checks == 1
		}}, nil)
		require.EqualValues(t, "stale", result.Status)
		require.Nil(t, saved)
		require.EqualValues(t, 0, adds)
		require.Len(t, state.Scopes["scope"], 0)
	})

	t.Run("changed after trusted add", func(t *testing.T) {
		fixture := newRegistrationFixture(t, "[]")
		current := true
		fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
			call := command.Name + " " + strings.Join(command.Args, " ")
			switch call {
			case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
				return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
			case "herdr machine add --label sbx · demo demo.sbx":
				return "Saved SSH machine machine-1. Remote server is ready.\n", nil
			case "herdr machine list --json":
				current = false
				return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
			default:
				return "", fmt.Errorf("unexpected call: %s", call)
			}
		}
		fixture.rebuildClients()
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, saved := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, Current: func() bool { return current }}, nil)
		items := state.Scopes["scope"]
		require.EqualValues(t, "stale", result.Status)
		require.NotNil(t, saved)
		require.Len(t, items, 1)
		require.EqualValues(t, "machine-1", items[0].ProfileID)
		require.False(t, items[0].Pending)
	})

	t.Run("changed during setup", func(t *testing.T) {
		fixture := newRegistrationFixture(t, "[]")
		fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
			call := command.Name + " " + strings.Join(command.Args, " ")
			switch call {
			case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
				return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
			case "herdr machine add --label sbx · demo demo.sbx":
				return "Saved SSH machine machine-1. Remote server is ready.\n", nil
			case "herdr machine list --json":
				return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
			case "herdr --machine machine-1 status server":
				return "running\n", nil
			default:
				return "", fmt.Errorf("unexpected call: %s", call)
			}
		}
		fixture.rebuildClients()
		checks := 0
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, _ := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, Current: func() bool {
			checks++
			return checks < 5
		}}, nil)
		require.EqualValues(t, "stale", result.Status)
		require.Contains(t, result.Detail, "sandbox changed")
	})
}

func TestCatalogCapacityFailureClearsIntentForRetry(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	adds := 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			return "machine catalog capacity reached", errors.New("catalog full")
		default:
			return "", fmt.Errorf("unexpected command %s", call)
		}
	}
	fixture.rebuildClients()
	sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}
	for range 2 {
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, _ := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, nil)
		require.EqualValues(t, "catalog full", result.Status)
		require.Contains(t, result.Detail, "Remove an unused machine")
		require.Len(t, state.Scopes["scope"], 0, "capacity failure left intent: %#v", state.Scopes["scope"])
	}
	require.Equal(t, 2, adds, "machine add calls=%d, want a retry", adds)
}

func TestUncertainAddRecoversUniqueCatalogEntryWithoutDuplicate(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	adds, lists, readinessChecks := 0, 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH_9.9", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/registration-retry.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1", nil
		case "herdr machine list --json":
			lists++
			if adds == 0 {
				return "[]", nil
			}
			return `[{"id":"manual-1","label":"demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			return "connection closed", errors.New("connection closed")
		case "herdr --machine manual-1 status server":
			readinessChecks++
			return "running", nil
		default:
			return "", fmt.Errorf("unexpected command %q", call)
		}
	}
	fixture.rebuildClients()
	var first strings.Builder
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), &first))
	require.Contains(t, first.String(), "outcome is uncertain", "first reconcile=%s", first.String())
	var second strings.Builder
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), &second))
	require.Contains(t, second.String(), "READY")
	require.EqualValues(t, 1, adds)
	require.EqualValues(t, 0, readinessChecks)
	require.EqualValues(t, 2, lists)
	state, err := store.LoadState()
	require.NoError(t, err)
	item := state.Scopes[store.ScopeID("/tmp/registration-retry.sock")][0]
	require.False(t, item.Pending)
	require.EqualValues(t, "manual-1", item.ProfileID)
	require.EqualValues(t, "demo", item.Label)
}

func TestProfilePersistenceFailureDoesNotClaimReadiness(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	stateDir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	adds, readinessChecks := 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH_9.9", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/persistence.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1", nil
		case "herdr machine list --json":
			if adds == 0 {
				return "[]", nil
			}
			return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			require.NoError(t, os.Rename(stateDir, stateDir+"-moved"))
			require.NoError(t, os.WriteFile(stateDir, []byte("not a directory"), 0600))
			return "Saved SSH machine machine-1. Remote server is ready.", nil
		case "herdr --machine machine-1 status server":
			readinessChecks++
			return "running", nil
		default:
			return "", fmt.Errorf("unexpected command %q", call)
		}
	}
	fixture.rebuildClients()
	var output strings.Builder
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), &output))
	require.Contains(t, output.String(), "ASSOCIATION WARNING")
	require.EqualValues(t, 0, readinessChecks)

	_, err := os.Stat(filepath.Join(stateDir+"-moved", "state.json"))
	require.NoError(t, err, "registration intent was not retained: %v", err)
}

func TestRemoteHerdrVersionAndBrokenPathAreLeftUnchanged(t *testing.T) {
	for _, scenario := range []string{"too old", "broken path"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRegistrationFixture(t, "[]")
			fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				require.EqualValues(t, "ssh", command.Name)
				require.NotEqualValues(t, 0, len(command.Args))
				require.False(t, command.Args[len(command.Args)-1] != remoteHerdrStatusShell())
				if scenario == "too old" {
					return remoteHerdrOnPath + "\nherdr 0.8.0\n", nil
				}
				return remoteHerdrConflict + "\n", nil
			}
			fixture.rebuildClients()
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
			result, saved := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}, Current: func() bool { return true }}, nil)
			require.EqualValues(t, "setup required", result.Status)
			require.Nil(t, saved)
			require.Len(t, state.Scopes["scope"], 0)
			require.False(t, scenario == "broken path" && !strings.Contains(result.Detail, "left unchanged"), "broken path detail=%s", result.Detail)
		})
	}
}

func TestCooldownRetainsInstallationFailure(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	statusChecks, architectureChecks := 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.Join(command.Args, " ") == "--version" {
			return "herdr 0.9.3", nil
		}
		if command.Name == "ssh" && len(command.Args) > 0 {
			switch command.Args[len(command.Args)-1] {
			case remoteHerdrStatusShell():
				statusChecks++
				return missingRemoteHerdr + "\n", nil
			case "uname -sm":
				architectureChecks++
				return "Linux x86_64", nil
			}
		}
		return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
	}
	fixture.rebuildClients()
	fixture.service.releaseTransport = releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("denied")), Request: request}, nil
	})
	sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
	first, _ := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, nil)
	require.EqualValues(t, "setup required", first.Status)
	require.Contains(t, first.Detail, "HTTP 403")
	second, _ := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, nil)
	require.EqualValues(t, "setup required", second.Status)
	require.Contains(t, second.Detail, "on cooldown")
	require.Contains(t, second.Detail, "HTTP 403")
	require.EqualValues(t, 2, statusChecks)
	require.EqualValues(t, 1, architectureChecks)
}

func TestFailedRecreationRetainsCompletedGuestState(t *testing.T) {
	for _, scenario := range []string{"cancel before add", "deterministic add failure"} {
		t.Run(scenario, func(t *testing.T) {
			testFailedRecreationRetainsCompletedGuestStateScenario(t, scenario)
		})
	}
}

func testFailedRecreationRetainsCompletedGuestStateScenario(t *testing.T, scenario string) {
	t.Helper()

	f := newRegistrationFixture(t, "[]")
	checked := time.Now()
	previous := store.Tracked{SandboxID: "one", Target: "demo.sbx", Label: "Previous", ProjectWorkspaceDone: true, GuestSync: &store.GuestSyncObservation{Fingerprint: "completed", CheckedAt: checked, Status: "ready"}}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {previous}}}
	require.NoError(t, store.SaveState(state))
	adds := 0
	f.runner.Execute = func(_ context.Context, c process.Command) (string, error) {
		call := c.Name + " " + strings.Join(c.Args, " ")
		switch call {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.3", nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			return "machine catalog capacity reached", errors.New("catalog full")
		default:
			return "", fmt.Errorf("unexpected repeated guest work: %s", call)
		}
	}
	f.rebuildClients()
	current := func() bool {
		if scenario != "cancel before add" {
			return true
		}
		saved, err := store.LoadState()
		require.NoError(t, err)
		return !saved.Scopes["scope"][0].Pending
	}
	outcome, _ := f.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "one", Name: "demo", Status: "running", Workspace: "/workspace/demo"}, Current: current}, nil)
	require.False(t, scenario == "cancel before add" && (outcome.Status != "stale" || adds != 0), "cancel=%+v adds=%d", outcome, adds)
	require.False(t, scenario == "deterministic add failure" && (outcome.Status != "catalog full" || adds != 1), "failure=%+v adds=%d", outcome, adds)
	saved, err := store.LoadState()
	require.NoError(t, err)
	require.Len(t, saved.Scopes["scope"], 1, "guest history removed: %+v", saved)
	item := saved.Scopes["scope"][0]
	require.False(t, item.Pending)
	require.EqualValues(t, "", item.ProfileID)
	require.True(t, item.ProjectWorkspaceDone)
	require.NotNil(t, item.GuestSync)
	require.EqualValues(t, "completed", item.GuestSync.Fingerprint)
	require.True(t, item.GuestSync.CheckedAt.Equal(checked))
}
