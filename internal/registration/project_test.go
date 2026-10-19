package registration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestProjectWorkspaceRegistration(t *testing.T) {
	for _, scenario := range []string{"new", "fresh server", "existing pane", "no project", "invalid directory", "missing directory", "ambiguous created", "ambiguous absent", "rejected", "replacement during resolve", "replacement during create", "cancel during resolve"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRegistrationFixture(t, "[]")
			const cwd = "/guest/project spaces ' $(touch nope) "
			sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running", Workspace: cwd}
			switch scenario {
			case "invalid directory", "missing directory", "replacement during resolve", "cancel during resolve":
				sandbox.WorkspaceNeedsProbe = true
			}
			if scenario == "no project" {
				sandbox.Workspace = ""
			}
			setFixtureInventory(fixture, sandbox)
			adds, creates, resolves := 0, 0, 0
			bootstrapWorkspaces := 0
			label := sandboxMachineLabel("demo")
			paneExists := scenario == "existing pane"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				joined := strings.Join(command.Args, " ")
				if command.Name == "ssh" && strings.Contains(joined, "--session default status server --json") {
					return fmt.Sprintf(`{"running":%t}`, scenario != "fresh server"), nil
				}
				if command.Name == "ssh" && strings.Contains(joined, "HERDR_STARTUP_CWD=") {
					bootstrapWorkspaces++
					paneExists = true
					return fmt.Sprintf(`{"result":{"panes":[{"workspace_id":"w1","cwd":%q}]}}`, cwd), nil
				}
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
					return `{"status":"running","socket":"/tmp/project-test.sock"}`, nil
				case "sbx diagnose --json":
					return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
				case "ssh -G herdr-sandbox-probe.sbx":
					return "proxycommand sbx ssh proxy %n", nil
				case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
					return remoteHerdrOnPath + "\nherdr 0.9.1\n", nil
				case "herdr machine list --json":
					if adds == 0 {
						return "[]", nil
					}
					return fmt.Sprintf(`[{"id":"machine-1","label":%q,"target":"demo.sbx","session":"default"}]`, label), nil
				case "herdr machine add --label sbx · demo demo.sbx":
					if scenario == "fresh server" && !paneExists {
						bootstrapWorkspaces++
					}
					adds++
					return "Saved SSH machine machine-1. Remote server is ready.", nil
				case "herdr --machine machine-1 status server":
					return "running", nil
				case "sbx exec demo -- pwd -P":
					resolves++
					switch scenario {
					case "invalid directory":
						return "/guest/../bad\n", nil
					case "missing directory":
						return "", errors.New("workspace missing")
					case "replacement during resolve":
						updated := sandbox
						updated.ID = "replacement"
						setFixtureInventory(fixture, updated)
					case "cancel during resolve":
						cancel()
					}
					return cwd + "\n", nil
				case "herdr --machine machine-1 pane list":
					if paneExists {
						data, _ := json.Marshal(map[string]any{"result": map[string]any{"panes": []any{map[string]any{"workspace_id": "ws_existing", "cwd": cwd}}}})
						return string(data), nil
					}
					return `{"result":{"panes":[]}}`, nil
				}
				if command.Name == "herdr" && len(command.Args) >= 7 && strings.Join(command.Args[:4], " ") == "--machine machine-1 workspace create" {
					expected := []string{"--machine", "machine-1", "workspace", "create", "--cwd", cwd, "--label", "demo", "--no-focus"}
					if strings.Join(command.Args, "\x00") != strings.Join(expected, "\x00") {
						t.Fatalf("unsafe create argv: %#v", command.Args)
					}
					creates++
					saved, err := store.LoadState()
					if err != nil {
						t.Fatal(err)
					}
					if saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0].ProjectWorkspacePending != cwd {
						t.Fatal("missing persisted creation intent")
					}
					if scenario == "ambiguous absent" {
						return "", errors.New("lost response")
					}
					if scenario == "rejected" {
						return `{"error":{"code":"invalid_cwd","message":"gone"}}`, errors.New("exit 1")
					}
					paneExists = true
					if scenario == "ambiguous created" {
						return "", errors.New("lost response")
					}
					if scenario == "replacement during create" {
						updated := sandbox
						updated.ID = "replacement"
						setFixtureInventory(fixture, updated)
					}
					data, _ := json.Marshal(map[string]any{"result": map[string]any{"workspace": map[string]any{"workspace_id": "ws_new"}, "root_pane": map[string]any{"cwd": cwd}}})
					return string(data), nil
				}
				return "", fmt.Errorf("unexpected command %q", command.Name+" "+joined)
			}
			fixture.rebuildClients()
			if scenario == "upgrade" {
				adds = 1
				if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{store.ScopeID("/tmp/project-test.sock"): {{SandboxID: sandbox.ID, Label: label, Target: "demo.sbx", ProfileID: "machine-1"}}}}); err != nil {
					t.Fatal(err)
				}
			}
			var output strings.Builder
			if err := fixture.service.ReconcileContext(ctx, &output); err != nil {
				t.Fatal(err)
			}
			saved, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			item := saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0]
			if adds != 1 || item.ProfileID != "machine-1" {
				t.Fatalf("ownership lost: adds=%d item=%#v", adds, item)
			}
			expectedResolves := 0
			if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
				expectedResolves = 1
			}
			switch scenario {
			case "fresh server":
				if bootstrapWorkspaces+creates != 1 || resolves != expectedResolves {
					t.Fatalf("new machine opened %d workspaces: %d bootstrap and %d project, probes=%d", bootstrapWorkspaces+creates, bootstrapWorkspaces, creates, resolves)
				}
			case "new", "existing pane":
				if !item.ProjectWorkspaceDone {
					t.Fatalf("not completed: %s", output.String())
				}
				paneExists = false
				for range 2 {
					if err := fixture.service.ReconcileContext(ctx, &output); err != nil {
						t.Fatal(err)
					}
				}
				expected := 1
				if scenario == "existing pane" {
					expected = 0
				}
				if creates != expected || resolves != expectedResolves || adds != 1 {
					t.Fatalf("repeated setup: creates=%d resolves=%d adds=%d", creates, resolves, adds)
				}
			case "ambiguous created":
				if item.ProjectWorkspacePending != cwd || item.ProjectWorkspaceDone {
					t.Fatalf("intent lost: %#v", item)
				}
				if err := fixture.service.ReconcileContext(ctx, &output); err != nil {
					t.Fatal(err)
				}
				saved, _ = store.LoadState()
				if !saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0].ProjectWorkspaceDone || creates != 1 {
					t.Fatalf("recovery failed: %s", output.String())
				}
			case "ambiguous absent":
				if err := fixture.service.ReconcileContext(ctx, &output); err != nil {
					t.Fatal(err)
				}
				if creates != 1 || !strings.Contains(output.String(), "previous creation is unresolved") {
					t.Fatalf("unsafe retry: %s", output.String())
				}
			case "rejected":
				if item.ProjectWorkspacePending != "" {
					t.Fatal("explicit rejection left uncertain intent")
				}
				if err := fixture.service.ReconcileContext(ctx, &output); err != nil {
					t.Fatal(err)
				}
				if creates != 2 {
					t.Fatal("explicit rejection was not retryable")
				}
			case "no project":
				if creates != 0 || resolves != 0 {
					t.Fatal("created without project")
				}
			case "replacement during create":
				if item.ProjectWorkspaceDone || item.ProjectWorkspacePending != cwd {
					t.Fatal("replacement completed stale setup")
				}
			case "replacement during resolve", "cancel during resolve":
				if creates != 0 {
					t.Fatal("created workspace after identity changed")
				}
			}
		})
	}
}

func setFixtureInventory(fixture *registrationFixture, sandbox sbx.Sandbox) {
	item := map[string]any{"uuid": sandbox.ID, "name": sandbox.Name, "status": sandbox.Status, "workspace": sandbox.Workspace}
	if sandbox.WorkspaceNeedsProbe {
		item["labels"] = map[string]string{"com.docker.sandbox.usesWorktree": "true"}
	}
	data, _ := json.Marshal([]map[string]any{item})
	fixture.inventory = string(data)
}

func TestInitialProjectStartupRetriesSavedIntent(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	const cwd = "/guest/project"
	sb := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running", Workspace: cwd}
	probes, resolves, starts := 0, 0, 0
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := strings.Join(command.Args, " ")
		switch {
		case command.Name == "ssh" && strings.Contains(joined, "--session default status server --json"):
			probes++
			return `{"running":false}`, nil
		case command.Name == "sbx" && joined == "exec demo -- pwd -P":
			resolves++
			return cwd + "\n", nil
		case command.Name == "ssh" && strings.Contains(joined, "HERDR_STARTUP_CWD="):
			starts++
			saved, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Scopes["scope"]) != 1 || saved.Scopes["scope"][0].ProjectWorkspacePending != cwd {
				t.Fatal("startup intent was not saved before server launch")
			}
			if starts == 1 {
				return "", errors.New("SSH disconnected after server start")
			}
			return `{"result":{"panes":[{"workspace_id":"w1","cwd":"/guest/project"}]}}`, nil
		default:
			return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
		}
	}
	f.rebuildClients()
	st := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
	current := func() bool { return true }
	if _, err := f.service.prepareInitialProjectWorkspace(context.Background(), "scope", &st, sb, current); err == nil {
		t.Fatal("startup failure ignored")
	}
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	// A saved startup intent wins over changed or uncertain inventory metadata.
	sb.Workspace = "/different/project"
	sb.WorkspaceNeedsProbe = true
	got, err := f.service.prepareInitialProjectWorkspace(context.Background(), "scope", &st, sb, current)
	expectedResolves := 0
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		expectedResolves = 1
	}
	if err != nil || got != cwd || probes != 1 || resolves != expectedResolves || starts != 2 || len(st.Scopes["scope"]) != 1 {
		t.Fatalf("cwd=%q err=%v probes=%d resolves=%d starts=%d state=%#v", got, err, probes, resolves, starts, st)
	}
}

func TestProjectDirectoryUsesInventoryOrGuestFallback(t *testing.T) {
	for _, test := range []struct {
		name       string
		workspace  string
		probe      bool
		output     string
		commandErr error
		wantErr    bool
	}{
		{"inventory", "/project", false, "/guest/project\n", nil, false},
		{"legacy", "/host/project", true, "/guest/project\n", nil, false},
		{"relative inventory", "project", false, "/guest/project\n", nil, false},
		{"unclean inventory", "/project/../other", false, "/guest/project\n", nil, false},
		{"control in inventory", "/project\nnoise", false, "/guest/project\n", nil, false},
		{"unknown inventory", "", false, "/guest/project\n", nil, false},
		{"invalid guest", "project", false, "/guest/../project\n", nil, true},
		{"probe failure", "project", false, "workspace unavailable", errors.New("exit 1"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			calls := 0
			f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				if command.Name != "sbx" || strings.Join(command.Args, " ") != "exec demo -- pwd -P" {
					t.Fatalf("unexpected command: %#v", command)
				}
				calls++
				return test.output, test.commandErr
			}
			f.rebuildClients()
			got, err := f.service.projectDirectory(context.Background(), sbx.Sandbox{Name: "demo", Workspace: test.workspace, WorkspaceNeedsProbe: test.probe})
			want, wantCalls := "/guest/project", 1
			if test.name == "inventory" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
				want, wantCalls = "/project", 0
			}
			if (err != nil) != test.wantErr || (!test.wantErr && got != want) || calls != wantCalls {
				t.Fatalf("cwd=%q err=%v probes=%d", got, err, calls)
			}
		})
	}
}
