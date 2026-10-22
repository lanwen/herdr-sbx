package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandErrorDetails(t *testing.T) {
	const forwarding = "machine 'CM': remote binary discovery failed: remote herdr does not support machine API forwarding; update herdr on this machine"
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name, output, code, message string
		compatibility               bool
	}{
		{"structured", `{"error":{"code":"remote_unsupported","message":"` + forwarding + `"}}`, "remote_unsupported", forwarding, true},
		{"rust wrapper", `Error: Custom { kind: Other, error: "` + forwarding + `" }`, "", forwarding, true},
		{"pinned herdr spelling", `Error: Custom { kind: Other, error: "remote Herdr does not support machine API forwarding; update Herdr on this machine" }`, "", "remote Herdr does not support machine API forwarding; update Herdr on this machine", true},
		{"escaped rust wrapper", `Error: Custom { kind: Other, error: "workspace \"/project\" is missing" }`, "", `workspace "/project" is missing`, false},
		{"other herdr error", `{"error":{"code":"invalid_cwd","message":"workspace is missing"}}`, "invalid_cwd", "workspace is missing", false},
		{"different versions", `{"error":{"code":"other","message":"local herdr 0.9.3; remote herdr 0.9.2"}}`, "other", "local herdr 0.9.3; remote herdr 0.9.2", false},
		{"malformed json", `{"error":{"code":42,"message":"` + forwarding + `"}}`, "", "", false},
		{"unwrapped text", forwarding, "", "", false},
		{"incomplete wrapper", `Error: Custom { kind: Other, error: "` + forwarding, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parseCommandError(cause, tc.output)
			require.ErrorIs(t, err, cause)
			require.False(t, MachineForwardingUnsupported(err) != tc.compatibility)
			var upstream *CommandError
			if tc.message == "" {
				require.NotErrorAs(t, err, &upstream)
				require.False(t, err != cause)
				return
			}
			require.ErrorAs(t, err, &upstream)
			require.False(t, upstream.Code != tc.code)
			require.False(t, upstream.Message != tc.message)
			require.True(t, strings.HasPrefix(err.Error(), "herdr"))
			require.False(t, tc.code != "" && !strings.Contains(err.Error(), "["+tc.code+"]"), "upstream code lost: %v", err)
		})
	}
}

func TestWorkspaceCommandsPreserveHerdrErrors(t *testing.T) {
	for _, operation := range []string{"panes", "create"} {
		t.Run(operation, func(t *testing.T) {
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return `{"error":{"code":"invalid_cwd","message":"workspace is missing"}}`, nil
			}})
			var err error
			if operation == "panes" {
				_, _, err = client.Panes(context.Background(), "machine-1")
			} else {
				var response WorkspaceResult
				response, _, err = client.CreateWorkspace(context.Background(), "machine-1", "/project", "demo")
				require.Equal(t, "herdr [invalid_cwd]: workspace is missing", response.Rejected, "rejection=%q", response.Rejected)
			}
			var upstream *CommandError
			require.ErrorAs(t, err, &upstream)
			require.EqualValues(t, "invalid_cwd", upstream.Code)
		})
	}
}

func TestStatusPopupSizing(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_ID", "lanwen.herdr-sbx")
	calls := 0
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		calls++
		want := "plugin pane open --plugin lanwen.herdr-sbx --entrypoint status --placement popup --focus --width 110 --height 32"
		require.EqualValues(t, "herdr", cmd.Name)
		require.False(t, strings.Join(cmd.Args, " ") != want)
		return "", nil
	}})

	err := client.OpenPane(context.Background(), "status", "popup")
	require.NoError(t, err)
	require.EqualValues(t, 1, calls)
}

func TestMachineAndPaneCatalogs(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "machine list --json" {
			return `[{"id":"m1","label":"demo","target":"demo.sbx","session":"default"}]`, nil
		}
		return `{"result":{"panes":[{"workspace_id":"w1","cwd":"/project with space "}]}}`, nil
	}})
	machines, _, err := client.ListMachines(context.Background())
	require.NoError(t, err)
	require.Len(t, machines, 1)
	require.EqualValues(t, "m1", machines[0].ID)
	require.EqualValues(t, "demo.sbx", machines[0].Target)
	require.EqualValues(t, "default", machines[0].Session)
	panes, _, err := client.Panes(context.Background(), "m1")
	require.NoError(t, err)
	require.Len(t, panes, 1)
	require.EqualValues(t, "/project with space ", panes[0].CWD)
}

func TestLocalPluginCatalog(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		require.Equal(t, "plugin list --json", strings.Join(cmd.Args, " "), "unexpected plugin command: %#v", cmd)
		return `{"result":{"plugins":[{"plugin_id":"example.alpha","plugin_root":"/plugins/alpha","manifest_path":"/plugins/alpha/custom.toml","version":"1.2.3","enabled":true},{"plugin_id":"example.beta","plugin_root":"/plugins/beta","manifest_path":"/plugins/beta/plugin.toml","version":"4.5.6","enabled":false}]}}`, nil
	}})
	plugins, err := client.LocalPlugins(context.Background())
	require.NoError(t, err)
	require.Len(t, plugins, 2)
	require.False(t, plugins[0] != (PluginInstall{ID: "example.alpha", Root: "/plugins/alpha", ManifestPath: "/plugins/alpha/custom.toml", Version: "1.2.3", Enabled: true}))
	require.EqualValues(t, "example.beta", plugins[1].ID)
	require.False(t, plugins[1].Enabled)
}

func TestCatalogRejectsMalformedResponses(t *testing.T) {
	for _, output := range []string{`null`, `{}`, `[null]`, `[{}]`, `[1]`, `[{"id":42}]`} {
		{
			_, err := parseMachines(output)
			assert.Error(t, err, "machine list accepted malformed response %s", output)
		}
	}
	for _, output := range []string{`null`, `{}`, `{"result":{"plugins":null}}`, `{"result":{"plugins":[null]}}`, `{"result":{"plugins":[{}]}}`, `{"result":{"plugins":[1]}}`, `{"result":{"plugins":[{"enabled":"yes"}]}}`} {
		{
			_, err := parsePlugins(output)
			assert.Error(t, err, "plugin list accepted malformed response %s", output)
		}
	}
	for _, output := range []string{`null`, `{}`, `{"result":{"panes":null}}`, `{"result":{"panes":[null]}}`, `{"result":{"panes":[{}]}}`, `{"result":{"panes":[1]}}`, `{"result":{"panes":[{"cwd":42}]}}`} {
		{
			_, err := parsePanes(output)
			assert.Error(t, err, "pane list accepted malformed response %s", output)
		}
	}
}

func TestSessionCountRejectsNullEntries(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		require.Equal(t, "session list --json", strings.Join(cmd.Args, " "), "unexpected session command: %#v", cmd)
		return `{"sessions":[null]}`, nil
	}})

	_, err := client.RunningSessionCount(context.Background())
	require.Error(t, err, "null session was treated as no running session")
}
