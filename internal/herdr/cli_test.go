package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
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
			if !errors.Is(err, cause) || MachineForwardingUnsupported(err) != tc.compatibility {
				t.Fatalf("error=%v compatibility=%t", err, MachineForwardingUnsupported(err))
			}
			var upstream *CommandError
			if tc.message == "" {
				if errors.As(err, &upstream) || err != cause {
					t.Fatalf("unknown output classified as herdr error: %v", err)
				}
				return
			}
			if !errors.As(err, &upstream) || upstream.Code != tc.code || upstream.Message != tc.message || !strings.HasPrefix(err.Error(), "herdr") {
				t.Fatalf("upstream=%#v error=%v", upstream, err)
			}
			if tc.code != "" && !strings.Contains(err.Error(), "["+tc.code+"]") {
				t.Fatalf("upstream code lost: %v", err)
			}
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
				if response.Rejected != "herdr [invalid_cwd]: workspace is missing" {
					t.Fatalf("rejection=%q", response.Rejected)
				}
			}
			var upstream *CommandError
			if !errors.As(err, &upstream) || upstream.Code != "invalid_cwd" {
				t.Fatalf("error=%v", err)
			}
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
		if cmd.Name != "herdr" || strings.Join(cmd.Args, " ") != want {
			t.Fatalf("unexpected popup command: %#v", cmd)
		}
		return "", nil
	}})
	if err := client.OpenPane(context.Background(), "status", "popup"); err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestMachineAndPaneCatalogs(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "machine list --json" {
			return `[{"id":"m1","label":"demo","target":"demo.sbx","session":"default"}]`, nil
		}
		return `{"result":{"panes":[{"workspace_id":"w1","cwd":"/project with space "}]}}`, nil
	}})
	machines, _, err := client.ListMachines(context.Background())
	if err != nil || len(machines) != 1 || machines[0].ID != "m1" || machines[0].Target != "demo.sbx" || machines[0].Session != "default" {
		t.Fatalf("machines=%#v err=%v", machines, err)
	}
	panes, _, err := client.Panes(context.Background(), "m1")
	if err != nil || len(panes) != 1 || panes[0].CWD != "/project with space " {
		t.Fatalf("panes=%#v err=%v", panes, err)
	}
}

func TestLocalPluginCatalog(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") != "plugin list --json" {
			t.Fatalf("unexpected plugin command: %#v", cmd)
		}
		return `{"result":{"plugins":[{"plugin_id":"example.alpha","plugin_root":"/plugins/alpha","manifest_path":"/plugins/alpha/custom.toml","version":"1.2.3","enabled":true},{"plugin_id":"example.beta","plugin_root":"/plugins/beta","manifest_path":"/plugins/beta/plugin.toml","version":"4.5.6","enabled":false}]}}`, nil
	}})
	plugins, err := client.LocalPlugins(context.Background())
	if err != nil || len(plugins) != 2 {
		t.Fatalf("plugins=%#v err=%v", plugins, err)
	}
	if plugins[0] != (PluginInstall{ID: "example.alpha", Root: "/plugins/alpha", ManifestPath: "/plugins/alpha/custom.toml", Version: "1.2.3", Enabled: true}) || plugins[1].ID != "example.beta" || plugins[1].Enabled {
		t.Fatalf("plugins=%#v", plugins)
	}
}

func TestCatalogRejectsMalformedResponses(t *testing.T) {
	for _, output := range []string{`null`, `{}`, `[null]`, `[{}]`, `[1]`, `[{"id":42}]`} {
		if _, err := parseMachines(output); err == nil {
			t.Errorf("machine list accepted malformed response %s", output)
		}
	}
	for _, output := range []string{`null`, `{}`, `{"result":{"plugins":null}}`, `{"result":{"plugins":[null]}}`, `{"result":{"plugins":[{}]}}`, `{"result":{"plugins":[1]}}`, `{"result":{"plugins":[{"enabled":"yes"}]}}`} {
		if _, err := parsePlugins(output); err == nil {
			t.Errorf("plugin list accepted malformed response %s", output)
		}
	}
	for _, output := range []string{`null`, `{}`, `{"result":{"panes":null}}`, `{"result":{"panes":[null]}}`, `{"result":{"panes":[{}]}}`, `{"result":{"panes":[1]}}`, `{"result":{"panes":[{"cwd":42}]}}`} {
		if _, err := parsePanes(output); err == nil {
			t.Errorf("pane list accepted malformed response %s", output)
		}
	}
}

func TestSessionCountRejectsNullEntries(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") != "session list --json" {
			t.Fatalf("unexpected session command: %#v", cmd)
		}
		return `{"sessions":[null]}`, nil
	}})
	if _, err := client.RunningSessionCount(context.Background()); err == nil {
		t.Fatal("null session was treated as no running session")
	}
}
