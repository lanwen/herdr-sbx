package registration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestReaddedMachineUsesProjectWorkspace(t *testing.T) {
	for _, test := range []struct {
		name, server string
		external     bool
	}{
		{name: "Remove then Enable with fresh server", server: "fresh server"},
		{name: "Remove then Enable with existing project pane", server: "existing pane"},
		{name: "externally removed entry with fresh server", server: "fresh server", external: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newProjectReaddFixture(t, test.server)
			ctx := context.Background()
			if test.external {
				f.profile = ""
				require.NoError(t, store.SetEnabled(f.scope, f.commands.sandbox.ID, store.Enabled))
				var output strings.Builder
				require.NoError(t, f.fixture.service.ReconcileContext(ctx, &output))
			} else {
				require.NoError(t, f.fixture.service.RemoveEntry(ctx, f.scope, f.commands.sandbox.ID))
				require.NoError(t, f.fixture.service.SetEnabled(ctx, f.scope, f.commands.sandbox.ID, store.Enabled))
			}
			f.assertProjectRestored()
		})
	}
}

type projectReaddFixture struct {
	fixture    *registrationFixture
	commands   *projectWorkspaceCommands
	scope      string
	profile    string
	operations []string
	paneLists  int
}

func newProjectReaddFixture(t *testing.T, server string) *projectReaddFixture {
	t.Helper()
	fixture := newRegistrationFixture(t, "[]")
	sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running", Workspace: "/guest/project"}
	setFixtureInventory(fixture, sandbox)
	commands := &projectWorkspaceCommands{t: t, fixture: fixture, scenario: server, cwd: sandbox.Workspace, sandbox: sandbox, label: sandboxMachineLabel(sandbox.Name), paneExists: server == "existing pane"}
	f := &projectReaddFixture{fixture: fixture, commands: commands, scope: store.ScopeID("/tmp/project-test.sock"), profile: "removed-machine"}
	fixture.runner.Execute = f.execute
	fixture.rebuildClients()
	item := store.Tracked{SandboxID: sandbox.ID, ProfileID: f.profile, Target: "demo.sbx", Label: commands.label, ProjectWorkspaceDone: true}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: {item}}}))
	require.NoError(t, store.SetEnabled(f.scope, sandbox.ID, store.Disabled))
	return f
}

func (f *projectReaddFixture) execute(ctx context.Context, command process.Command) (string, error) {
	call := command.Name + " " + strings.Join(command.Args, " ")
	switch call {
	case "herdr machine list --json":
		return f.catalog()
	case "herdr machine remove removed-machine":
		f.profile = ""
		return "", nil
	case "herdr machine add --label sbx · demo demo.sbx":
		f.operations = append(f.operations, "add")
		f.profile = "machine-1"
		f.commands.adds++
		return "Saved SSH machine machine-1. Remote server is ready.", nil
	case "herdr --machine machine-1 pane list":
		f.paneLists++
	}
	if command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "HERDR_STARTUP_CWD=") {
		require.Contains(f.commands.t, strings.Join(command.Args, " "), "HERDR_STARTUP_CWD='/guest/project'", "new server did not receive the project working directory")
		f.operations = append(f.operations, "project startup")
	}
	return f.commands.execute(ctx, command)
}

func (f *projectReaddFixture) catalog() (string, error) {
	if f.profile == "" {
		return "[]", nil
	}
	machines := []herdr.Machine{{ID: f.profile, Label: f.commands.label, Target: "demo.sbx", Session: "default", Enabled: boolPointer(f.profile == "machine-1")}}
	data, err := json.Marshal(machines)
	return string(data), err
}

func (f *projectReaddFixture) assertProjectRestored() {
	t := f.commands.t
	t.Helper()
	require.Equal(t, 1, f.commands.adds)
	if f.commands.scenario == "fresh server" {
		require.Equal(t, []string{"project startup", "add"}, f.operations, "a fresh server must start in the project before registration")
		require.Equal(t, 1, f.commands.bootstrapWorkspaces)
	} else {
		require.Positive(t, f.paneLists, "existing project pane must be verified after re-registration")
		require.Zero(t, f.commands.bootstrapWorkspaces)
	}
	require.Zero(t, f.commands.creates, "re-registration duplicated the project workspace")
	saved, err := store.LoadState()
	require.NoError(t, err)
	items := saved.Scopes[f.scope]
	require.Len(t, items, 1, "unexpected registration state: %+v", items)
	require.Equal(t, "machine-1", items[0].ProfileID)
	require.True(t, items[0].ProjectWorkspaceDone)
}
