package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInspectDoesNotListWhenDaemonStopped(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	var calls []string
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch call {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"stopped","socket":"/tmp/sandboxd.sock"}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	p := inspect()
	if !hasLevel(p.Checks, "Sandbox daemon", blocked) {
		t.Fatalf("daemon check = %#v", p.Checks)
	}
	if !hasLevel(p.Checks, "Docker authorization", unknown) {
		t.Fatalf("authorization check = %#v", p.Checks)
	}
	if !hasLevel(p.Checks, "Remote readiness", unknown) {
		t.Fatalf("remote readiness check = %#v", p.Checks)
	}
	for _, call := range calls {
		if call == "sbx ls --json" {
			t.Fatal("inspection listed sandboxes while daemon was stopped")
		}
	}
}

func TestReconcileRegistersOnceAndPersistsOwnership(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var adds, machineLists int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/sandboxd.sock"}`, nil
		case "sbx ls --json":
			return `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			adds++
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr --machine machine-1 status server":
			return "running\n", nil
		case "herdr machine list --json":
			machineLists++
			if adds == 0 {
				return "[]", nil
			}
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	if err := reconcile(testWriter{t}); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(testWriter{t}); err != nil {
		t.Fatal(err)
	}
	if adds != 1 {
		t.Fatalf("machine add calls = %d, want 1", adds)
	}
	st, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	items := st.Scopes[scopeID("/tmp/sandboxd.sock")]
	if len(items) != 1 || items[0].ProfileID != "machine-1" || items[0].Pending {
		t.Fatalf("state = %#v", items)
	}
	if machineLists < 3 {
		t.Fatalf("machine lists = %d, want repeated inspection", machineLists)
	}
}

func TestManualMachineIsNotAdopted(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	st := state{Version: 1, Scopes: map[string][]tracked{}}
	got, saved := registerPrepared("scope", &st, []machine{{ID: "manual", Label: "demo", Target: "demo.sbx"}}, sandbox{ID: "sandbox-1", Name: "demo", Status: "running"})
	if got.Status != "ownership conflict" || saved != nil {
		t.Fatalf("result = %#v, saved = %#v", got, saved)
	}
}

func TestParsersAcceptCLIResultEnvelopes(t *testing.T) {
	sandboxes, err := parseSandboxes(`{"result":{"sandboxes":[{"id":"s1","name":"demo","state":"running"}]}}`)
	if err != nil || len(sandboxes) != 1 || sandboxes[0].ID != "s1" {
		t.Fatalf("sandboxes = %#v, err = %v", sandboxes, err)
	}
	machines, err := parseMachines(`{"result":{"machines":[{"id":"m1","label":"demo","target":"demo.sbx","remote_session":"default"}]}}`)
	if err != nil || len(machines) != 1 || machines[0].ID != "m1" {
		t.Fatalf("machines = %#v, err = %v", machines, err)
	}
	if ownedMachineByID(machines, "m1", "demo", "demo.sbx") == nil || ownedMachineByID([]machine{{ID: "m1", Label: "demo", Target: "demo.sbx", Session: "other"}}, "m1", "demo", "demo.sbx") != nil {
		t.Fatal("machine ownership did not require the expected target and default session")
	}
	var daemon map[string]any
	if err := decodeJSON("warning: local settings unavailable\n{\"status\":\"stopped\"}\n", &daemon); err != nil || daemon["status"] != "stopped" {
		t.Fatalf("daemon = %#v, err = %v", daemon, err)
	}
}

func TestSandboxAppScopeIsExplicit(t *testing.T) {
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "herdr-sbx-test")
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name != "sbx" || strings.Join(args, " ") != "--app-name herdr-sbx-test ls --json" {
			t.Fatalf("command = %s %s", name, strings.Join(args, " "))
		}
		return "[]", nil
	})
	if _, err := runSBX("ls", "--json"); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessFailureKeepsOwnedProfileForRetry(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var adds, readinessChecks int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/sandboxd.sock"}`, nil
		case "sbx ls --json":
			return `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			adds++
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr machine list --json":
			if adds == 0 {
				return "[]", nil
			}
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr --machine machine-1 status server":
			readinessChecks++
			if readinessChecks == 1 {
				return "remote unavailable", errors.New("bridge failed")
			}
			return "running\n", nil
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	var first bytes.Buffer
	if err := reconcile(&first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "saved machine is not ready") {
		t.Fatalf("first reconciliation = %s", first.String())
	}
	st, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	item := st.Scopes[scopeID("/tmp/sandboxd.sock")][0]
	if item.ProfileID != "machine-1" || item.Pending {
		t.Fatalf("state after readiness failure = %#v", item)
	}

	var second bytes.Buffer
	if err := reconcile(&second); err != nil {
		t.Fatal(err)
	}
	if adds != 1 || !strings.Contains(second.String(), "already registered and reachable") {
		t.Fatalf("second reconciliation = %s, adds = %d", second.String(), adds)
	}
}

func TestCatalogCapacityFailureClearsIntentForRetry(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var adds int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/sandboxd.sock"}`, nil
		case "sbx ls --json":
			return `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine list --json":
			return "[]", nil
		case "herdr machine add --label demo demo.sbx":
			adds++
			return "machine catalog capacity reached", errors.New("catalog full")
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	var output bytes.Buffer
	if err := reconcile(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "CATALOG FULL") {
		t.Fatalf("reconciliation = %s", output.String())
	}
	st, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Scopes[scopeID("/tmp/sandboxd.sock")]) != 0 {
		t.Fatalf("capacity failure left state = %#v", st)
	}
	if err := reconcile(ioDiscard{}); err != nil {
		t.Fatal(err)
	}
	if adds != 2 {
		t.Fatalf("machine add calls = %d, want retry", adds)
	}
}

func TestFailedAddNeverAdoptsManualProfileAfterward(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var adds, readinessChecks int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/sandboxd.sock"}`, nil
		case "sbx ls --json":
			return `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine list --json":
			if adds == 0 {
				return "[]", nil
			}
			return `[{"profile_id":"manual-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr machine add --label demo demo.sbx":
			adds++
			return "connection closed", errors.New("connection closed")
		case "herdr --machine manual-1 status server":
			readinessChecks++
			return "", nil
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	var first bytes.Buffer
	if err := reconcile(&first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "outcome is uncertain") {
		t.Fatalf("first reconciliation = %s", first.String())
	}
	var second bytes.Buffer
	if err := reconcile(&second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "OWNERSHIP CONFLICT") || adds != 1 || readinessChecks != 0 {
		t.Fatalf("second reconciliation = %s, adds = %d, readiness = %d", second.String(), adds, readinessChecks)
	}
	st, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	item := st.Scopes[scopeID("/tmp/sandboxd.sock")][0]
	if !item.Pending || item.ProfileID != "" {
		t.Fatalf("manual machine was adopted: %#v", item)
	}
}

func TestProfilePersistenceFailureRemainsOwnershipConflict(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_BIN_PATH", "herdr")
	var readinessChecks int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		switch call {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			if err := os.Rename(stateDir, stateDir+"-moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stateDir, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr machine list --json":
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr --machine machine-1 status server":
			readinessChecks++
			return "running\n", nil
		default:
			return "", errors.New("unexpected command: " + call)
		}
	})

	st := state{Version: 1, Scopes: map[string][]tracked{}}
	got, saved := registerPrepared("scope", &st, nil, sandbox{ID: "sandbox-1", Name: "demo", Status: "running"})
	if got.Status != "ownership conflict" || saved == nil || readinessChecks != 0 {
		t.Fatalf("result = %#v, saved = %#v, readiness = %d", got, saved, readinessChecks)
	}
}

func TestPrerequisiteClassification(t *testing.T) {
	checks := inventoryFailureChecks(errors.New("failed"), "docker login service unavailable")
	if len(checks) != 1 || checks[0].Name != "Docker authorization" || checks[0].Level != blocked {
		t.Fatalf("authorization checks = %#v", checks)
	}
	checks = inventoryFailureChecks(errors.New("failed"), "sandboxd socket connection refused")
	if len(checks) != 2 || checks[0].Name != "Sandbox inventory" || checks[1].Name != "Docker authorization" {
		t.Fatalf("backend checks = %#v", checks)
	}
	if sshSetting("proxycommand ssh jump.example\n", "proxycommand") == "" || strings.Contains(sshSetting("proxycommand ssh jump.example\n", "proxycommand"), "sbx ssh proxy") {
		t.Fatal("arbitrary proxy was treated as a managed sandbox proxy")
	}
}

func TestSBXMinimumVersionIsEnforced(t *testing.T) {
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.44.9\n", nil
		default:
			return "", errors.New("unexpected command")
		}
	})
	if got := executableCheck("sbx", "sbx", minSBXVersion); got.Level != blocked {
		t.Fatalf("sbx check = %#v", got)
	}
}

func TestRepairRunsOnlyTheChosenInteractiveCommandThenRefreshes(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	var statusChecks int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			statusChecks++
			return `{"status":"running","socket":"/tmp/sandboxd.sock"}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	original := interactiveRun
	t.Cleanup(func() { interactiveRun = original })
	var got string
	interactiveRun = func(name string, args ...string) error {
		got = name + " " + strings.Join(args, " ")
		return nil
	}

	var output bytes.Buffer
	if err := repair("ssh", &output); err != nil {
		t.Fatal(err)
	}
	if got != "sbx setup ssh" || statusChecks != 1 || !strings.Contains(output.String(), "finished.") || !strings.Contains(output.String(), "Refreshing status") {
		t.Fatalf("command = %q, checks = %d, output = %s", got, statusChecks, output.String())
	}
}

func TestRepairCancellationRefreshesWithoutRetry(t *testing.T) {
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"stopped","socket":"/tmp/sandboxd.sock"}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		default:
			return "", errors.New("unexpected command")
		}
	})
	original := interactiveRun
	t.Cleanup(func() { interactiveRun = original })
	interactiveRun = func(string, ...string) error { return errInteractiveCanceled }
	var output bytes.Buffer
	if err := repair("login", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "canceled") || !strings.Contains(output.String(), "Refreshing status") {
		t.Fatalf("output = %s", output.String())
	}
}

func TestLifecycleReaderAcceptsInitialReplayAndUnknownFields(t *testing.T) {
	var got []lifecycleEvent
	stream := "{\"type\":\"sandbox.lifecycle\",\"action\":\"started\",\"sandbox_id\":\"s1\",\"sandbox_name\":\"demo\",\"extra\":true}\n" +
		"{\"type\":\"sandbox.lifecycle\",\"action\":\"sync\",\"unknown\":\"ok\"}\n"
	if err := readLifecycleLines(strings.NewReader(stream), func(event lifecycleEvent) bool {
		got = append(got, event)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != "started" || got[0].SandboxID != "s1" || got[1].Action != "sync" {
		t.Fatalf("events = %#v", got)
	}
}

func TestLifecycleSchedulerIgnoresOtherEventTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := newLifecycleScheduler(ctx, "scope", "/tmp/sandboxd.sock", ioDiscard{})
	if !scheduler.enqueue(lifecycleEvent{Type: "other.event", Action: "deleted", SandboxID: "sandbox-1"}) {
		t.Fatal("unknown event type was rejected")
	}
	scheduler.mu.Lock()
	_, retired := scheduler.retired["sandbox-1"]
	_, pending := scheduler.deletes["sandbox-1"]
	scheduler.mu.Unlock()
	cancel()
	scheduler.wait()
	if retired || pending {
		t.Fatal("unknown event type changed lifecycle state")
	}
}

func TestLifecycleDeletionRemovesOnlyOwnedProfileInItsScope(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	st := state{Version: 1, Scopes: map[string][]tracked{
		"scope-a": {{SandboxID: "sandbox-1", Label: "demo", Target: "demo.sbx", ProfileID: "machine-1"}},
		"scope-b": {{SandboxID: "sandbox-1", Label: "other", Target: "other.sbx", ProfileID: "machine-2"}},
	}}
	if err := saveState(st); err != nil {
		t.Fatal(err)
	}
	var removes []string
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "herdr machine list --json":
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr machine remove machine-1":
			removes = append(removes, strings.Join(args, " "))
			return "", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	scheduler := newLifecycleScheduler(context.Background(), "scope-a", "/tmp/sandboxd.sock", ioDiscard{})
	scheduler.removeDeleted(lifecycleEvent{Action: "deleted", SandboxID: "sandbox-1", SandboxName: "demo"})
	if len(removes) != 1 {
		t.Fatalf("removes = %#v", removes)
	}
	st, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Scopes["scope-a"]) != 0 || len(st.Scopes["scope-b"]) != 1 {
		t.Fatalf("state = %#v", st.Scopes)
	}
}

func TestLifecycleDeletionRetainsMismatchedProfile(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	st := state{Version: 1, Scopes: map[string][]tracked{"scope": {{SandboxID: "sandbox-1", Label: "demo", Target: "demo.sbx", ProfileID: "machine-1"}}}}
	if err := saveState(st); err != nil {
		t.Fatal(err)
	}
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "herdr" && strings.Join(args, " ") == "machine list --json" {
			return `[{"profile_id":"machine-1","label":"manual","ssh_target":"manual.example","remote_session":"default"}]`, nil
		}
		t.Fatal("unexpected command: " + name + " " + strings.Join(args, " "))
		return "", nil
	})
	var output bytes.Buffer
	scheduler := newLifecycleScheduler(context.Background(), "scope", "/tmp/sandboxd.sock", &output)
	scheduler.removeDeleted(lifecycleEvent{Action: "deleted", SandboxID: "sandbox-1"})
	if !strings.Contains(output.String(), "retained state") {
		t.Fatalf("output = %s", output.String())
	}
	st, err := loadState()
	if err != nil || len(st.Scopes["scope"]) != 1 {
		t.Fatalf("state = %#v, err = %v", st, err)
	}
}

func TestLifecycleQueueKeepsDeletionWhenFull(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	st := state{Version: 1, Scopes: map[string][]tracked{"scope": {{SandboxID: "sandbox-1", Label: "demo", Target: "demo.sbx", ProfileID: "machine-1"}}}}
	if err := saveState(st); err != nil {
		t.Fatal(err)
	}
	removed := make(chan struct{}, 1)
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "herdr machine list --json":
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr machine remove machine-1":
			removed <- struct{}{}
			return "", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	originalInventory := inventory
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	inventory = func(string) ([]sandbox, error) {
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
		return nil, nil
	}
	t.Cleanup(func() { inventory = originalInventory })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler := newLifecycleScheduler(ctx, "scope", "/tmp/sandboxd.sock", ioDiscard{})
	if !scheduler.enqueue(lifecycleEvent{Type: "sandbox.lifecycle", Action: "started", SandboxID: "hold", SandboxName: "hold"}) {
		t.Fatal("initial event was rejected")
	}
	<-entered
	for i := 0; i < cap(scheduler.queue); i++ {
		if !scheduler.enqueue(lifecycleEvent{Type: "sandbox.lifecycle", Action: "started", SandboxID: "queued-" + strings.Repeat("x", i+1)}) {
			t.Fatal("queue filled before expected")
		}
	}
	if scheduler.enqueue(lifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "sandbox-1"}) {
		t.Fatal("full queue accepted deletion")
	}
	close(release)
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("deletion was lost after queue overflow")
	}
	cancel()
	scheduler.wait()
}

func TestRegistrationStopsBeforeMachineAddWhenSandboxChanges(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	var adds, checks int
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			adds++
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	st := state{Version: 1, Scopes: map[string][]tracked{}}
	got, saved := registerPreparedContext(context.Background(), "scope", &st, nil, sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
		checks++
		return checks == 1
	})
	if got.Status != "stale" || saved != nil || adds != 0 || len(st.Scopes["scope"]) != 0 {
		t.Fatalf("result = %#v, saved = %#v, adds = %d, state = %#v", got, saved, adds, st.Scopes)
	}
}

func TestRegistrationPersistsTrustedProfileWhenSandboxChangesAfterAdd(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr machine list --json":
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	checks := 0
	st := state{Version: 1, Scopes: map[string][]tracked{}}
	got, saved := registerPreparedContext(context.Background(), "scope", &st, nil, sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
		checks++
		return checks < 4
	})
	items := st.Scopes["scope"]
	if got.Status != "stale" || saved == nil || len(items) != 1 || items[0].ProfileID != "machine-1" || items[0].Pending {
		t.Fatalf("result = %#v, saved = %#v, state = %#v", got, saved, items)
	}
}

func TestLifecycleGapRequestsCatchupWithoutCrossStreamComparison(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := newLifecycleScheduler(ctx, "scope", "/tmp/sandboxd.sock", ioDiscard{})
	cancel()
	scheduler.wait()
	first, third := uint64(1), uint64(3)
	scheduler.enqueue(lifecycleEvent{Type: "sandbox.lifecycle", Action: "sync", Seq: &first})
	scheduler.enqueue(lifecycleEvent{Type: "sandbox.lifecycle", Action: "sync", Seq: &third})
	scheduler.mu.Lock()
	gap := scheduler.catchup
	scheduler.mu.Unlock()
	if !gap {
		t.Fatal("sequence gap did not request catch-up")
	}
	scheduler.beginStream()
	scheduler.mu.Lock()
	reset := scheduler.lastSeq == nil
	scheduler.mu.Unlock()
	if !reset {
		t.Fatal("new stream retained the previous sequence")
	}
}

func TestLifecycleDeletionKeepsPendingIntentVisible(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	st := state{Version: 1, Scopes: map[string][]tracked{"scope": {{SandboxID: "old", Label: "demo", Target: "demo.sbx", Pending: true}}}}
	if err := saveState(st); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	scheduler := newLifecycleScheduler(context.Background(), "scope", "/tmp/sandboxd.sock", &output)
	if !scheduler.removeDeleted(lifecycleEvent{Action: "deleted", SandboxID: "old"}) || !strings.Contains(output.String(), "unresolved registration intent") {
		t.Fatalf("output = %s", output.String())
	}
	st, err := loadState()
	if err != nil || len(st.Scopes["scope"]) != 1 {
		t.Fatalf("state = %#v, err = %v", st, err)
	}
}

func TestLifecycleDeletionClearsStateWhenOwnedProfileIsAlreadyAbsent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	st := state{Version: 1, Scopes: map[string][]tracked{"scope": {{SandboxID: "old", Label: "demo", Target: "demo.sbx", ProfileID: "machine-1"}}}}
	if err := saveState(st); err != nil {
		t.Fatal(err)
	}
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "herdr" && strings.Join(args, " ") == "machine list --json" {
			return "[]", nil
		}
		return "", errors.New("unexpected command")
	})
	scheduler := newLifecycleScheduler(context.Background(), "scope", "/tmp/sandboxd.sock", ioDiscard{})
	if !scheduler.removeDeleted(lifecycleEvent{Action: "deleted", SandboxID: "old"}) {
		t.Fatal("absent profile did not clear state")
	}
	st, err := loadState()
	if err != nil || len(st.Scopes["scope"]) != 0 {
		t.Fatalf("state = %#v, err = %v", st, err)
	}
}

func TestRegistrationDoesNotReportReadyAfterFinalIdentityChange(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx herdr --version":
			return "herdr 0.9.1\n", nil
		case "herdr machine add --label demo demo.sbx":
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr machine list --json":
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx","remote_session":"default"}]`, nil
		case "herdr --machine machine-1 status server":
			return "running\n", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
	checks := 0
	st := state{Version: 1, Scopes: map[string][]tracked{}}
	got, _ := registerPreparedContext(context.Background(), "scope", &st, nil, sandbox{ID: "old", Name: "demo", Status: "running"}, func() bool {
		checks++
		return checks < 5
	})
	if got.Status != "stale" || !strings.Contains(got.Detail, "remote readiness") {
		t.Fatalf("result = %#v", got)
	}
}

func TestInspectShowsUnresolvedDeletedRegistration(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	socket := "/tmp/sandboxd.sock"
	if err := saveState(state{Version: 1, Scopes: map[string][]tracked{scopeID(socket): {{SandboxID: "old", Label: "demo", Target: "demo.sbx", Pending: true}}}}); err != nil {
		t.Fatal(err)
	}
	withInspectionRunner(t, socket)
	p := inspect()
	if !hasLevel(p.Checks, "Registration ownership", unknown) {
		t.Fatalf("checks = %#v", p.Checks)
	}
}

func TestEmptyInventoryIsNotReady(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	withInspectionRunner(t, "/tmp/sandboxd.sock")
	original := inventory
	inventory = func(string) ([]sandbox, error) { return nil, nil }
	t.Cleanup(func() { inventory = original })
	p := inspect()
	if !hasLevel(p.Checks, "Sandbox inventory", unknown) || hasLevel(p.Checks, "Sandbox inventory", ready) {
		t.Fatalf("checks = %#v", p.Checks)
	}
}

func TestDaemonClientDoesNotKeepIdleConnections(t *testing.T) {
	transport, ok := daemonClient("/tmp/sandboxd.sock").Transport.(*http.Transport)
	if !ok || !transport.DisableKeepAlives {
		t.Fatalf("transport = %#v", transport)
	}
}

func withInspectionRunner(t *testing.T, socket string) {
	t.Helper()
	withRunner(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx --version":
			return "", errors.New("unknown flag")
		case "sbx version":
			return "sbx version: v0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"` + socket + `"}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		default:
			return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	})
}

func withRunner(t *testing.T, replacement func(context.Context, string, ...string) (string, error)) {
	t.Helper()
	original := run
	originalInventory := inventory
	run = replacement
	inventory = func(string) ([]sandbox, error) {
		return []sandbox{{ID: "sandbox-1", Name: "demo", Status: "running"}}, nil
	}
	t.Cleanup(func() {
		run = original
		inventory = originalInventory
	})
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
