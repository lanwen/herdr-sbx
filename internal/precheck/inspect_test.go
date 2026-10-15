package precheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

type checkTransport func(*http.Request) (*http.Response, error)

func (f checkTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type checkFixture struct {
	inspector                  *Inspector
	version, daemon, inventory string
	diagnostic, proxy          string
	inventoryCalls, authCalls  int
}

func newCheckFixture(t *testing.T) *checkFixture {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "herdr-sbx-test-checks")
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sbx", "ssh"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	f := &checkFixture{
		version: "0.45.1", daemon: `{"status":"running","socket":"/tmp/checks.sock"}`,
		inventory:  `[{"id":"old","name":"demo","status":"running"}]`,
		diagnostic: `{"checks":[{"name":"Authentication","status":"pass"}]}`,
		proxy:      "sbx ssh proxy %n",
	}
	runner := process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		args := cmd.Args
		scoped := len(args) > 1 && args[0] == "--app-name" && args[1] == "herdr-sbx-test-checks"
		if scoped {
			args = args[2:]
		}
		call := cmd.Name + " " + strings.Join(args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx version":
			return "sbx version: v" + f.version, nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			if !scoped {
				t.Fatal("daemon check lost test app scope")
			}
			return f.daemon, nil
		case "sbx diagnose --json":
			if !scoped {
				t.Fatal("authentication check lost test app scope")
			}
			f.authCalls++
			return f.diagnostic, errors.New("unrelated diagnostic failed")
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand " + f.proxy, nil
		}
		t.Errorf("passive inspection ran unexpected command: %s", call)
		return "", errors.New("unexpected command")
	}}
	sandboxes := sbx.NewWithTransport(runner, checkTransport(func(r *http.Request) (*http.Response, error) {
		f.inventoryCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/sandbox" {
			t.Errorf("unexpected passive request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.inventory))}, nil
	}))
	f.inspector = New(sandboxes, herdr.New(runner), runner)
	return f
}

func assertCheck(t *testing.T, report Result, name string, want Level) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			if check.Level != want {
				t.Fatalf("%s = %#v, want %s", name, check, want)
			}
			return
		}
	}
	t.Fatalf("missing %s in %#v", name, report.Checks)
}

func TestInspectDoesNotListWhenDaemonStopped(t *testing.T) {
	f := newCheckFixture(t)
	f.daemon = `{"status":"stopped","socket":"/tmp/checks.sock"}`
	report := f.inspector.Inspect()
	assertCheck(t, report, "Sandbox daemon", Blocked)
	assertCheck(t, report, "Docker authorization", Unknown)
	assertCheck(t, report, "Remote readiness", Unknown)
	if f.inventoryCalls != 0 || f.authCalls != 0 {
		t.Fatal("stopped daemon caused an inventory or authorization query")
	}
}

func TestPrerequisiteClassification(t *testing.T) {
	checks := ClassifyInventoryError(errors.New("failed"), "docker login service unavailable")
	if len(checks) != 1 || checks[0].Name != "Docker authorization" || checks[0].Level != Blocked {
		t.Fatalf("authorization checks = %#v", checks)
	}
	checks = ClassifyInventoryError(errors.New("failed"), "sandboxd socket connection refused")
	if len(checks) != 2 || checks[0].Name != "Sandbox inventory" || checks[1].Name != "Docker authorization" {
		t.Fatalf("backend checks = %#v", checks)
	}
	if proxy := sshSetting("proxycommand ssh jump.example\n", "proxycommand"); proxy == "" || sandboxSSHProxy.MatchString(proxy) {
		t.Fatal("arbitrary SSH proxy accepted as managed")
	}
}

func TestSBXMinimumVersionIsEnforced(t *testing.T) {
	f := newCheckFixture(t)
	f.version = "0.44.9"
	report := f.inspector.Inspect()
	assertCheck(t, report, "sbx", Blocked)
	assertCheck(t, report, "Sandbox capability", Blocked)
}

func TestInspectShowsUnresolvedDeletedRegistration(t *testing.T) {
	f := newCheckFixture(t)
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{
		store.ScopeID("/tmp/checks.sock"): {{SandboxID: "old", Label: "demo", Target: "demo.sbx", Pending: true}},
	}}); err != nil {
		t.Fatal(err)
	}
	assertCheck(t, f.inspector.Inspect(), "Registration ownership", Unknown)
}

func TestInspectDockerSignIn(t *testing.T) {
	for _, test := range []struct {
		name, diagnostic string
		want             Level
		empty            bool
	}{
		{"other failure", `{"checks":[{"name":"Authentication","status":"pass"},{"name":"Storage","status":"fail"}]}`, Ready, false},
		{"empty inventory", `{"checks":[{"name":"Authentication","status":"pass"}]}`, Ready, true},
		{"signed out", `{"checks":[{"name":"Authentication","status":"fail"}]}`, Blocked, false},
		{"unavailable", `{"checks":[{"name":"Authentication","status":"warn"}]}`, Unknown, false},
		{"skipped", `{"checks":[{"name":"Authentication","status":"skip"}]}`, Unknown, false},
		{"missing", `{"checks":[{"name":"Storage","status":"pass"}]}`, Unknown, false},
		{"unsupported", "", Unknown, false},
		{"malformed", `{"checks":`, Unknown, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCheckFixture(t)
			f.diagnostic = test.diagnostic
			if test.empty {
				f.inventory = `[]`
			}
			assertCheck(t, f.inspector.Inspect(), "Docker authorization", test.want)
			if f.authCalls != 1 {
				t.Fatalf("diagnostic called %d times", f.authCalls)
			}
		})
	}
}

func TestInspectRecognizesSandboxSSHProxy(t *testing.T) {
	for _, test := range []struct {
		proxy string
		want  Level
	}{
		{`"/opt/homebrew/bin/sbx" ssh proxy %n`, Ready},
		{`'/path with spaces/sbx' ssh proxy %n`, Ready},
		{`/usr/local/bin/sbx ssh proxy %n`, Ready},
		{`sbx ssh proxy %n`, Ready},
		{`other-sbx ssh proxy %n`, Unknown},
		{`echo sbx ssh proxy %n`, Unknown},
		{`none`, Blocked},
	} {
		t.Run(test.proxy, func(t *testing.T) {
			f := newCheckFixture(t)
			f.proxy = test.proxy
			assertCheck(t, f.inspector.Inspect(), "Sandbox SSH", test.want)
		})
	}
}

func TestEmptyInventoryIsNotReady(t *testing.T) {
	f := newCheckFixture(t)
	f.inventory = `[]`
	assertCheck(t, f.inspector.Inspect(), "Sandbox inventory", Unknown)
}
