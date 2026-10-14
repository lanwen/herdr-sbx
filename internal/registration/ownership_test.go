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

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
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
		result, saved := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
			checks++
			return checks == 1
		})
		if result.Status != "stale" || saved != nil || adds != 0 || len(state.Scopes["scope"]) != 0 {
			t.Fatalf("result=%#v saved=%#v adds=%d state=%#v", result, saved, adds, state.Scopes)
		}
	})

	t.Run("changed after trusted add", func(t *testing.T) {
		fixture := newRegistrationFixture(t, "[]")
		fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
			call := command.Name + " " + strings.Join(command.Args, " ")
			switch call {
			case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
				return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
			case "herdr machine add --label sbx · demo demo.sbx":
				return "Saved SSH machine machine-1. Remote server is ready.\n", nil
			case "herdr machine list --json":
				return `[{"profile_id":"machine-1","label":"sbx · demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
			default:
				return "", fmt.Errorf("unexpected call: %s", call)
			}
		}
		fixture.rebuildClients()
		checks := 0
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, saved := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
			checks++
			return checks < 4
		})
		items := state.Scopes["scope"]
		if result.Status != "stale" || saved == nil || len(items) != 1 || items[0].ProfileID != "machine-1" || items[0].Pending {
			t.Fatalf("result=%#v saved=%#v state=%#v", result, saved, items)
		}
	})

	t.Run("changed after readiness", func(t *testing.T) {
		fixture := newRegistrationFixture(t, "[]")
		fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
			call := command.Name + " " + strings.Join(command.Args, " ")
			switch call {
			case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
				return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
			case "herdr machine add --label sbx · demo demo.sbx":
				return "Saved SSH machine machine-1. Remote server is ready.\n", nil
			case "herdr machine list --json":
				return `[{"profile_id":"machine-1","label":"sbx · demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
			case "herdr --machine machine-1 status server":
				return "running\n", nil
			default:
				return "", fmt.Errorf("unexpected call: %s", call)
			}
		}
		fixture.rebuildClients()
		checks := 0
		state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
		result, _ := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
			checks++
			return checks < 5
		})
		if result.Status != "stale" || !strings.Contains(result.Detail, "remote readiness") {
			t.Fatalf("result=%#v", result)
		}
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
		result, _ := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sandbox, func() bool { return true })
		if result.Status != "catalog full" || !strings.Contains(result.Detail, "Remove an unused machine") {
			t.Fatalf("result=%#v", result)
		}
		if len(state.Scopes["scope"]) != 0 {
			t.Fatalf("capacity failure left intent: %#v", state.Scopes["scope"])
		}
	}
	if adds != 2 {
		t.Fatalf("machine add calls=%d, want a retry", adds)
	}
}

func TestSavedMachineReadinessCanBeRetriedWithoutLosingOwnership(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	readinessChecks := 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.Join(command.Args, " ") == "--machine machine-1 status server" {
			readinessChecks++
			if readinessChecks == 1 {
				return "remote unavailable", errors.New("bridge failed")
			}
			return "running\n", nil
		}
		return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
	}
	fixture.rebuildClients()
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
	item := store.Tracked{SandboxID: "sandbox-1", Label: sandboxMachineLabel("demo"), Target: "demo.sbx", ProfileID: "machine-1"}
	state.Scopes["scope"] = []store.Tracked{item}
	current := func() bool { return true }
	sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}
	first := fixture.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, current)
	if first.Status != "failed" || !strings.Contains(first.Detail, "Retry registration") || item.ProfileID != "machine-1" || item.Pending {
		t.Fatalf("first=%#v item=%#v", first, item)
	}
	second := fixture.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, current)
	if second.Status != "ready" || readinessChecks != 2 || item.ProfileID != "machine-1" {
		t.Fatalf("second=%#v checks=%d item=%#v", second, readinessChecks, item)
	}
}

func TestFailedAddCannotAdoptManualMachineOnRetry(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	adds, lists, readinessChecks := 0, 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx --version":
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
			return `[{"profile_id":"manual-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
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
	if err := fixture.service.ReconcileContext(context.Background(), &first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "outcome is uncertain") {
		t.Fatalf("first reconcile=%s", first.String())
	}
	var second strings.Builder
	if err := fixture.service.ReconcileContext(context.Background(), &second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "OWNERSHIP CONFLICT") || adds != 1 || readinessChecks != 0 || lists != 2 {
		t.Fatalf("second reconcile=%s adds=%d lists=%d readiness=%d", second.String(), adds, lists, readinessChecks)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	item := state.Scopes[store.ScopeID("/tmp/registration-retry.sock")][0]
	if !item.Pending || item.ProfileID != "" {
		t.Fatalf("manual machine was adopted: %#v", item)
	}
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
		case "sbx --version":
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
			return `[{"profile_id":"machine-1","label":"sbx · demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			if err := os.Rename(stateDir, stateDir+"-moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stateDir, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
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
	if err := fixture.service.ReconcileContext(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "OWNERSHIP CONFLICT") || readinessChecks != 0 {
		t.Fatalf("output=%s readiness=%d", output.String(), readinessChecks)
	}
	if _, err := os.Stat(filepath.Join(stateDir+"-moved", "state.json")); err != nil {
		t.Fatalf("registration intent was not retained: %v", err)
	}
}

func TestRemoteHerdrVersionAndBrokenPathAreLeftUnchanged(t *testing.T) {
	for _, scenario := range []string{"too old", "broken path"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRegistrationFixture(t, "[]")
			fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				if command.Name != "ssh" || len(command.Args) == 0 || command.Args[len(command.Args)-1] != remoteHerdrStatusShell() {
					t.Fatalf("remote setup unexpectedly ran a command: %s %v", command.Name, command.Args)
				}
				if scenario == "too old" {
					return remoteHerdrOnPath + "\nherdr 0.8.0\n", nil
				}
				return remoteHerdrConflict + "\n", nil
			}
			fixture.rebuildClients()
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
			result, saved := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}, func() bool { return true })
			if result.Status != "setup required" || saved != nil || len(state.Scopes["scope"]) != 0 {
				t.Fatalf("result=%#v saved=%#v", result, saved)
			}
			if scenario == "broken path" && !strings.Contains(result.Detail, "left unchanged") {
				t.Fatalf("broken path detail=%s", result.Detail)
			}
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
	first, _ := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sandbox, func() bool { return true })
	if first.Status != "setup required" || !strings.Contains(first.Detail, "HTTP 403") {
		t.Fatalf("first failure=%#v", first)
	}
	second, _ := fixture.service.registerPreparedContext(context.Background(), "scope", &state, nil, sandbox, func() bool { return true })
	if second.Status != "setup required" || !strings.Contains(second.Detail, "on cooldown") || !strings.Contains(second.Detail, "HTTP 403") {
		t.Fatalf("retry detail=%#v", second)
	}
	if statusChecks != 2 || architectureChecks != 1 {
		t.Fatalf("status checks=%d host downloads=%d", statusChecks, architectureChecks)
	}
}
