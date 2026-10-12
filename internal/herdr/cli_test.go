package herdr

import (
	"context"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

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

func TestParsersAcceptCLIResultEnvelopes(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "machine list --json" {
			return `{"result":{"machines":[{"id":"m1","label":"demo","target":"demo.sbx","remote_session":"default"}]}}`, nil
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
