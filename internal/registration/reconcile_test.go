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

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestReconcilePersistsOnlyVerifiedMachineAndDoesNotAddTwice(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	adds, lists, readinessChecks := 0, 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := strings.Join(command.Args, " ")
		switch command.Name + " " + joined {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx version":
			return "sbx version: 0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/test-sandboxd.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
		case "herdr machine list --json":
			lists++
			if adds == 0 {
				return `[]`, nil
			}
			return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr --machine machine-1 status server":
			readinessChecks++
			return "workspace directory no longer exists", errors.New("connection aborted")
		default:
			return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
		}
	}
	fixture.rebuildClients()

	for range 2 {
		if err := fixture.service.Reconcile(io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if readinessChecks != 0 {
		t.Fatalf("machine add already confirmed readiness; post-add checks=%d", readinessChecks)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	items := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")]
	if adds != 1 || lists < 3 || len(items) != 1 || items[0].ProfileID != "machine-1" || items[0].Pending {
		t.Fatalf("adds=%d lists=%d state=%#v", adds, lists, items)
	}
}

func TestManualMachineIsNotAdopted(t *testing.T) {
	fixture := newRegistrationFixture(t, `[]`)
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
	outcome, saved := fixture.service.registerPrepared("scope", &state,
		[]herdr.Machine{{ID: "manual", Label: "demo", Target: "demo.sbx", Session: "default"}},
		sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"})
	if outcome.Status != "ownership conflict" || outcome.Detail != "This sandbox is already a Herdr machine. It was added outside Herdr-sbx." || saved != nil {
		t.Fatalf("outcome=%#v saved=%#v", outcome, saved)
	}
	outcome, saved = fixture.service.registerPrepared("scope", &state,
		[]herdr.Machine{{ID: "other", Label: sandboxMachineLabel("demo"), Target: "other.sbx", Session: "default"}},
		sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"})
	if outcome.Status != "ownership conflict" || outcome.Detail != "Another Herdr machine already uses this sandbox's name. Review that machine in Herdr." || saved != nil {
		t.Fatalf("name collision outcome=%#v saved=%#v", outcome, saved)
	}
}

func TestUncertainMachineAddRemainsPendingForManualInspection(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := strings.Join(command.Args, " ")
		switch command.Name + " " + joined {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/test-sandboxd.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
		case "herdr machine list --json":
			return `[]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			return "", errors.New("connection dropped after request")
		default:
			return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
		}
	}
	fixture.rebuildClients()
	var output strings.Builder
	if err := fixture.service.Reconcile(&output); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	items := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")]
	if len(items) != 1 || !items[0].Pending || !strings.Contains(output.String(), "outcome is uncertain") {
		t.Fatalf("state=%#v output=%s", items, output.String())
	}
}

type registrationFixture struct {
	t         *testing.T
	service   *Service
	runner    *process.Runner
	inventory string
	transport http.RoundTripper
}

func newRegistrationFixture(t *testing.T, inventory string) *registrationFixture {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sbx", "ssh"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	fixture := &registrationFixture{t: t, inventory: inventory}
	fixture.runner = &process.Runner{}
	fixture.runner.Execute = func(context.Context, process.Command) (string, error) {
		return "", errors.New("unexpected command before fake runner is configured")
	}
	fixture.rebuildClients()
	return fixture
}

func (f *registrationFixture) rebuildClients() {
	fakeHTTP := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, f.inventory
		if request.URL.Path != "/sandbox" {
			status, body = http.StatusNotFound, "{}"
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	transport := f.transport
	if transport == nil {
		transport = fakeHTTP
	}
	sandboxClient := sbx.NewWithTransport(*f.runner, transport)
	herdrClient := herdr.New(*f.runner)
	inspector := precheck.New(sandboxClient, herdrClient, *f.runner)
	f.service = New(sandboxClient, herdrClient, inspector, *f.runner)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func remoteHerdrStatusShell() string {
	return "if command -v herdr >/dev/null 2>&1; then printf '" + remoteHerdrOnPath + "\\n'; herdr --version; elif [ -x \"$HOME/.local/bin/herdr\" ]; then printf '" + remoteHerdrFallback + "\\n'; \"$HOME/.local/bin/herdr\" --version; elif [ -e \"$HOME/.local/bin/herdr\" ] || [ -L \"$HOME/.local/bin/herdr\" ]; then printf '" + remoteHerdrConflict + "\\n'; else printf '" + missingRemoteHerdr + "\\n'; fi"
}

func TestReconcileRetriesPendingRegistrationWhenCatalogIsEmpty(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	scope := store.ScopeID("/tmp/test-sandboxd.sock")
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "sandbox-1", Label: "sbx · demo", Target: "demo.sbx", Pending: true, ProjectWorkspacePending: "/workspace/demo"}}}}); err != nil {
		t.Fatal(err)
	}
	adds, lists := 0, 0
	handedOff := false
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := strings.Join(command.Args, " ")
		switch command.Name + " " + joined {
		case "herdr --version":
			return "herdr 0.9.1\n", nil
		case "sbx version":
			return "sbx version: 0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/test-sandboxd.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
		case "herdr machine list --json":
			lists++
			if adds == 0 {
				return `[]`, nil
			}
			return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr --machine machine-1 pane list":
			return `{"result":{"panes":[{"workspace_id":"w1","cwd":"/workspace/demo"}]}}`, nil
		case "herdr --machine machine-1 status server":
			return "running\n", nil
		default:
			if command.Name == "ssh" && strings.HasSuffix(joined, "status server --json") {
				if !handedOff {
					return `{"running":true,"version":"0.9.4"}`, nil
				}
				return `{"running":true,"version":"0.9.1"}`, nil
			}
			if command.Name == "ssh" && strings.Contains(joined, "server live-handoff") {
				handedOff = true
				return "done", nil
			}
			if command.Name == "ssh" && strings.Contains(joined, "HERDR_STARTUP_CWD=") {
				if !handedOff {
					t.Fatal("queried project panes before version alignment")
				}
				return `{"result":{"panes":[{"workspace_id":"w1","cwd":"/workspace/demo"}]}}`, nil
			}
			return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
		}
	}
	fixture.rebuildClients()

	for range 2 {
		if err := fixture.service.Reconcile(io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	items := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")]
	if adds != 1 || lists < 3 || len(items) != 1 || items[0].ProfileID != "machine-1" || items[0].Pending {
		t.Fatalf("adds=%d lists=%d state=%#v", adds, lists, items)
	}
}
