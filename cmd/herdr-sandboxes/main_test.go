package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
			return "", nil
		case "herdr --machine machine-1 status server":
			return "running\n", nil
		case "herdr machine list --json":
			machineLists++
			if adds == 0 {
				return "[]", nil
			}
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx"}]`, nil
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
	machines, err := parseMachines(`{"result":{"machines":[{"id":"m1","label":"demo","target":"demo.sbx"}]}}`)
	if err != nil || len(machines) != 1 || machines[0].ID != "m1" {
		t.Fatalf("machines = %#v, err = %v", machines, err)
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
			return "", nil
		case "herdr machine list --json":
			if adds == 0 {
				return "[]", nil
			}
			return `[{"profile_id":"machine-1","label":"demo","ssh_target":"demo.sbx"}]`, nil
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

func withRunner(t *testing.T, replacement func(context.Context, string, ...string) (string, error)) {
	t.Helper()
	original := run
	run = replacement
	t.Cleanup(func() { run = original })
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
