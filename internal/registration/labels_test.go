package registration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPendingLabelOwnershipAndRecovery(t *testing.T) {
	for _, scenario := range []string{"target changed", "collision", "lost acknowledgement", "no-op rename", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRegistrationFixture(t, "[]")
			item := store.Tracked{SandboxID: "sandbox-1", Label: "original", PendingLabel: "Release desk", Target: "demo.sbx", ProfileID: "machine-1"}
			machine := herdr.Machine{ID: "machine-1", Label: "original", Target: "demo.sbx", Session: "default"}
			switch scenario {
			case "manual rename":
				machine.Label = "my workstation"
			case "target changed":
				machine.Target = "elsewhere.sbx"
			}
			machines := []herdr.Machine{machine}
			if scenario == "collision" {
				machines = append(machines, herdr.Machine{ID: "manual", Label: "Release desk", Target: "elsewhere"})
			}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
			require.NoError(t, store.SaveState(state))
			renames := 0
			fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				call := command.Name + " " + strings.Join(command.Args, " ")
				switch call {
				case "herdr machine rename machine-1 --label Release desk":
					renames++
					if scenario != "no-op rename" {
						machines[0].Label = "Release desk"
					}
					if scenario == "lost acknowledgement" {
						return "", errors.New("lost response")
					}
					return "", nil
				case "herdr machine list --json":
					return fmt.Sprintf(`[{"id":"machine-1","label":%q,"target":%q,"session":"default"}]`, machines[0].Label, machines[0].Target), nil
				default:
					return "", fmt.Errorf("unexpected %s", call)
				}
			}
			fixture.rebuildClients()
			current := func() bool { return scenario != "stale" }
			sandbox := sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}
			err := fixture.service.recoverMachineLabel(context.Background(), "scope", &state, machines, sandbox, &item, current)
			require.Error(t, err, "expected rename refusal or failure")
			if scenario == "lost acknowledgement" {
				saved, err := store.LoadState()
				require.NoError(t, err)
				item = saved.Scopes["scope"][0]
				require.Equal(t, "Release desk", item.PendingLabel, "rename intent lost")
				require.NoError(t, fixture.service.recoverMachineLabel(context.Background(), "scope", &saved, machines, sandbox, &item, current))
				require.EqualValues(t, "Release desk", item.Label)
				require.EqualValues(t, "", item.PendingLabel)
				require.EqualValues(t, 1, renames)
			} else if scenario == "no-op rename" {
				require.EqualValues(t, "original", item.Label)
				require.EqualValues(t, "Release desk", item.PendingLabel)
				require.EqualValues(t, 1, renames)
			} else if renames != 0 {
				t.Fatal("changed unowned or stale machine")
			}
		})
	}
}
