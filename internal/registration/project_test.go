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
	"github.com/stretchr/testify/require"
)

func TestProjectWorkspaceRegistration(t *testing.T) {
	for _, scenario := range []string{"new", "fresh server", "existing pane", "no project", "invalid directory", "missing directory", "ambiguous created", "ambiguous absent", "rejected", "replacement during resolve", "replacement during create", "cancel during resolve"} {
		t.Run(scenario, func(t *testing.T) {
			testProjectWorkspaceRegistrationScenario(t, scenario)
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
			require.NoError(t, err)
			require.Len(t, saved.Scopes["scope"], 1)
			require.False(t, saved.Scopes["scope"][0].ProjectWorkspacePending != cwd)
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
	{
		_, err := f.service.prepareInitialProjectWorkspace(context.Background(), "scope", &st, sb, current)
		require.Error(t, err, "startup failure ignored")
	}
	st, err := store.LoadState()
	require.NoError(t, err)
	// A saved startup intent wins over changed or uncertain inventory metadata.
	sb.Workspace = "/different/project"
	sb.WorkspaceNeedsProbe = true
	got, err := f.service.prepareInitialProjectWorkspace(context.Background(), "scope", &st, sb, current)
	expectedResolves := 0
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		expectedResolves = 1
	}
	require.NoError(t, err)
	require.False(t, got != cwd)
	require.EqualValues(t, 1, probes)
	require.False(t, resolves != expectedResolves)
	require.EqualValues(t, 2, starts)
	require.Len(t, st.Scopes["scope"], 1)
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
		{"older", "/host/project", true, "/guest/project\n", nil, false},
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
				require.EqualValues(t, "sbx", command.Name)
				require.EqualValues(t, "exec demo -- pwd -P", strings.Join(command.Args, " "))
				calls++
				return test.output, test.commandErr
			}
			f.rebuildClients()
			got, err := f.service.projectDirectory(context.Background(), sbx.Sandbox{Name: "demo", Workspace: test.workspace, WorkspaceNeedsProbe: test.probe})
			want, wantCalls := "/guest/project", 1
			if test.name == "inventory" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
				want, wantCalls = "/project", 0
			}
			require.False(t, (err != nil) != test.wantErr)
			require.False(t, !test.wantErr && got != want)
			require.False(t, calls != wantCalls)
		})
	}
}

func testProjectWorkspaceRegistrationScenario(t *testing.T, scenario string) {
	t.Helper()

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
	label := sandboxMachineLabel("demo")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	commands := &projectWorkspaceCommands{t: t, fixture: fixture, scenario: scenario, cwd: cwd, sandbox: sandbox, label: label, paneExists: scenario == "existing pane", cancel: cancel}
	fixture.runner.Execute = commands.execute
	fixture.rebuildClients()
	if scenario == "upgrade" {
		commands.adds = 1
		require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{store.ScopeID("/tmp/project-test.sock"): {{SandboxID: sandbox.ID, Label: label, Target: "demo.sbx", ProfileID: "machine-1"}}}}))
	}
	var output strings.Builder
	require.NoError(t, fixture.service.ReconcileContext(ctx, &output))
	saved, err := store.LoadState()
	require.NoError(t, err)
	item := saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0]
	require.EqualValues(t, 1, commands.adds)
	require.EqualValues(t, "machine-1", item.ProfileID)
	commands.assertOutcome(ctx, &output, item, saved)
}

type projectWorkspaceCommands struct {
	t                                            *testing.T
	fixture                                      *registrationFixture
	scenario, cwd, label                         string
	sandbox                                      sbx.Sandbox
	cancel                                       context.CancelFunc
	adds, creates, resolves, bootstrapWorkspaces int
	paneExists                                   bool
}

func (f *projectWorkspaceCommands) execute(_ context.Context, command process.Command) (string, error) {

	joined := strings.Join(command.Args, " ")
	if command.Name == "ssh" && strings.Contains(joined, "--session default status server --json") {
		return fmt.Sprintf(`{"running":%t}`, f.scenario != "fresh server"), nil
	}
	if command.Name == "ssh" && strings.Contains(joined, "HERDR_STARTUP_CWD=") {
		require.Positive(f.t, f.fixture.shellSetups, "initial pane started before sandbox shell setup")
		f.bootstrapWorkspaces++
		f.paneExists = true
		return fmt.Sprintf(`{"result":{"panes":[{"workspace_id":"w1","cwd":%q}]}}`, f.cwd), nil
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
		return f.catalog()
	case "herdr machine add --label sbx · demo demo.sbx":
		return f.add()
	case "herdr --machine machine-1 status server":
		return "running", nil
	case "sbx exec demo -- pwd -P":
		return f.resolve()
	case "herdr --machine machine-1 pane list":
		return f.panes()
	}
	if command.Name == "herdr" && len(command.Args) >= 7 && strings.Join(command.Args[:4], " ") == "--machine machine-1 workspace create" {
		require.Positive(f.t, f.fixture.shellSetups, "project workspace created before sandbox shell setup")
		return f.create(command)
	}
	return "", fmt.Errorf("unexpected command %q", command.Name+" "+joined)
}
func (f *projectWorkspaceCommands) catalog() (string, error) {

	if f.adds == 0 {
		return "[]", nil
	}
	return fmt.Sprintf(`[{"id":"machine-1","label":%q,"target":"demo.sbx","session":"default"}]`, f.label), nil

}

func (f *projectWorkspaceCommands) add() (string, error) {

	if f.scenario == "fresh server" && !f.paneExists {
		f.bootstrapWorkspaces++
	}
	f.adds++
	return "Saved SSH machine machine-1. Remote server is ready.", nil

}

func (f *projectWorkspaceCommands) resolve() (string, error) {

	f.resolves++
	switch f.scenario {
	case "invalid directory":
		return "/guest/../bad\n", nil
	case "missing directory":
		return "", errors.New("workspace missing")
	case "replacement during resolve":
		updated := f.sandbox
		updated.ID = "replacement"
		setFixtureInventory(f.fixture, updated)
	case "cancel during resolve":
		f.cancel()
	}
	return f.cwd + "\n", nil

}

func (f *projectWorkspaceCommands) panes() (string, error) {

	if f.paneExists {
		data, _ := json.Marshal(map[string]any{"result": map[string]any{"panes": []any{map[string]any{"workspace_id": "ws_existing", "cwd": f.cwd}}}})
		return string(data), nil
	}
	return `{"result":{"panes":[]}}`, nil
}

func (f *projectWorkspaceCommands) create(command process.Command) (string, error) {
	expected := []string{"--machine", "machine-1", "workspace", "create", "--cwd", f.cwd, "--label", "demo", "--no-focus"}
	require.Equal(f.t, strings.Join(expected, "\x00"), strings.Join(command.Args, "\x00"), "unsafe create argv: %#v", command.Args)
	f.creates++
	saved, err := store.LoadState()
	require.NoError(f.t, err)
	require.Equal(f.t, f.cwd, saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0].ProjectWorkspacePending, "missing persisted creation intent")
	if f.scenario == "ambiguous absent" {
		return "", errors.New("lost response")
	}
	if f.scenario == "rejected" {
		return `{"error":{"code":"invalid_cwd","message":"gone"}}`, errors.New("exit 1")
	}
	f.paneExists = true
	if f.scenario == "ambiguous created" {
		return "", errors.New("lost response")
	}
	if f.scenario == "replacement during create" {
		updated := f.sandbox
		updated.ID = "replacement"
		setFixtureInventory(f.fixture, updated)
	}
	data, _ := json.Marshal(map[string]any{"result": map[string]any{"workspace": map[string]any{"workspace_id": "ws_new"}, "root_pane": map[string]any{"cwd": f.cwd}}})
	return string(data), nil
}

func (f *projectWorkspaceCommands) assertOutcome(ctx context.Context, output *strings.Builder, item store.Tracked, saved store.State) {
	t := f.t
	t.Helper()
	expectedResolves := 0
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		expectedResolves = 1
	}
	switch f.scenario {
	case "fresh server":
		require.EqualValues(t, 1, f.bootstrapWorkspaces+f.creates)
		require.False(t, f.resolves != expectedResolves)
	case "new", "existing pane":
		require.True(t, item.ProjectWorkspaceDone, "not completed: %s", output.String())
		f.paneExists = false
		for range 2 {
			require.NoError(f.t, f.fixture.service.ReconcileContext(ctx, output))
		}
		expected := 1
		if f.scenario == "existing pane" {
			expected = 0
		}
		require.False(t, f.creates != expected)
		require.False(t, f.resolves != expectedResolves)
		require.EqualValues(t, 1, f.adds)
	case "ambiguous created":
		require.False(t, item.ProjectWorkspacePending != f.cwd)
		require.False(t, item.ProjectWorkspaceDone)
		require.NoError(f.t, f.fixture.service.ReconcileContext(ctx, output))
		saved, _ = store.LoadState()
		require.True(t, saved.Scopes[store.ScopeID("/tmp/project-test.sock")][0].ProjectWorkspaceDone)
		require.EqualValues(t, 1, f.creates)
	case "ambiguous absent":
		require.NoError(f.t, f.fixture.service.ReconcileContext(ctx, output))
		require.EqualValues(t, 1, f.creates)
		require.Contains(t, output.String(), "previous creation is unresolved")
	case "rejected":
		require.Equal(f.t, "", item.ProjectWorkspacePending, "explicit rejection left uncertain intent")
		require.NoError(f.t, f.fixture.service.ReconcileContext(ctx, output))
		require.Equal(f.t, 2, f.creates, "explicit rejection was not retryable")
	case "no project":
		require.EqualValues(t, 0, f.creates)
		require.EqualValues(t, 0, f.resolves)
	case "replacement during create":
		require.False(t, item.ProjectWorkspaceDone)
		require.False(t, item.ProjectWorkspacePending != f.cwd)
	case "replacement during resolve", "cancel during resolve":
		require.Equal(f.t, 0, f.creates, "created workspace after identity changed")
	}
}
