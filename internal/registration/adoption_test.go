package registration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestAdoptionRequiresUniqueUnownedTargetAndDefaultSession(t *testing.T) {
	sandbox := sbx.Sandbox{ID: "sb-1", Name: "demo", Status: "running"}
	machine := herdr.Machine{ID: "manual-1", Label: "My sandbox", Target: "demo.sbx", Session: "default"}
	for _, test := range []struct {
		name     string
		machines []herdr.Machine
		state    store.State
		want     int
	}{
		{name: "matching target", machines: []herdr.Machine{machine}, want: 1},
		{name: "label only", machines: []herdr.Machine{{ID: "wrong", Label: "sbx · demo", Target: "elsewhere", Session: "default"}}},
		{name: "different session", machines: []herdr.Machine{{ID: "manual-1", Target: "demo.sbx", Session: "another"}}},
		{name: "ambiguous target", machines: []herdr.Machine{machine, {ID: "other", Target: "demo.sbx", Session: "default"}}},
		{name: "owned elsewhere", machines: []herdr.Machine{machine}, state: store.State{Scopes: map[string][]store.Tracked{"other": {{SandboxID: "other", ProfileID: "manual-1", Target: "demo.sbx"}}}}},
		{name: "pending matching intent", machines: []herdr.Machine{machine}, state: store.State{Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "sb-1", Target: "demo.sbx", Pending: true}}}}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			choices := adoptionChoices("scope", "socket", []sbx.Sandbox{sandbox}, test.machines, test.state)
			if len(choices) != test.want {
				t.Fatalf("choices=%+v", choices)
			}
		})
	}
}

func TestAdoptPreservesMachineAndRejectsStaleSelection(t *testing.T) {
	for _, change := range []string{"none", "sandbox replaced", "machine changed", "already owned"} {
		t.Run(change, func(t *testing.T) {
			f := newRegistrationFixture(t, `[{"uuid":"sb-1","name":"demo","status":"running"}]`)
			catalog := `[{"id":"manual-1","label":"My sandbox","target":"demo.sbx","session":"default"}]`
			f.runner.Execute = func(_ context.Context, cmd process.Command) (string, error) {
				switch cmd.Name + " " + strings.Join(cmd.Args, " ") {
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
				case "herdr machine list --json":
					return catalog, nil
				default:
					t.Errorf("adoption attempted an unexpected or mutating command: %+v", cmd)
					return "", fmt.Errorf("unexpected command")
				}
			}
			f.rebuildClients()
			choices, err := f.service.AdoptionChoices(context.Background())
			if err != nil || len(choices) != 1 {
				t.Fatalf("choices=%+v err=%v", choices, err)
			}
			switch change {
			case "sandbox replaced":
				f.inventory = strings.ReplaceAll(f.inventory, "sb-1", "sb-2")
			case "machine changed":
				catalog = strings.ReplaceAll(catalog, "My sandbox", "Edited machine")
			case "already owned":
				if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{"another": {{ProfileID: "manual-1", SandboxID: "other", Target: "demo.sbx"}}}}); err != nil {
					t.Fatal(err)
				}
			}
			err = f.service.Adopt(context.Background(), choices[0])
			if change != "none" {
				if err == nil {
					t.Fatal("accepted stale selection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			items := state.Scopes[choices[0].Scope]
			if len(items) != 1 || items[0].ProfileID != "manual-1" || items[0].Label != "My sandbox" || items[0].Pending {
				t.Fatalf("ownership=%+v", items)
			}
			if ownedTrackedMachine([]herdr.Machine{choices[0].Machine}, items[0]) == nil {
				t.Fatal("adopted profile is not recognized by normal ownership checks")
			}
		})
	}
}
