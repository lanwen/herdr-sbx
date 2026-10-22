package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestMachineNativeEnabledObservation(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		known, want bool
	}{
		{"enabled", `,"enabled":true`, true, true},
		{"disabled", `,"enabled":false`, true, false},
		{"missing", ``, false, false},
		{"unknown", `,"enabled":null`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machines, err := parseMachines(`[{"id":"m1","label":"renamed","target":"demo.sbx","session":"default"` + tc.field + `}]`)
			if err != nil || len(machines) != 1 {
				t.Fatalf("machines=%#v err=%v", machines, err)
			}
			got := machines[0].Enabled
			if (got != nil) != tc.known || (got != nil && *got != tc.want) {
				t.Fatalf("native enabled=%v, want known=%t value=%t", got, tc.known, tc.want)
			}
		})
	}
	if _, err := parseMachines(`[{"id":"m1","enabled":"false"}]`); err == nil {
		t.Fatal("non-boolean native state was accepted")
	}
}

func TestSetMachineEnabledUsesOnlyHostCatalogCommand(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "herdr")
	for _, enabled := range []bool{false, true} {
		action := "disable"
		if enabled {
			action = "enable"
		}
		t.Run(action, func(t *testing.T) {
			calls := 0
			client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
				calls++
				if command.Name != "herdr" || strings.Join(command.Args, " ") != "machine "+action+" m1" || command.Env["HERDR_SOCKET_PATH"] != "isolated.sock" {
					t.Fatalf("unexpected operation: %#v", command)
				}
				return "saved", nil
			}})
			client.RebindSocket("isolated.sock")
			output, err := client.SetMachineEnabled(context.Background(), "m1", enabled)
			if err != nil || output != "saved" || calls != 1 {
				t.Fatalf("output=%q calls=%d err=%v", output, calls, err)
			}
		})
	}
}

func TestSetMachineEnabledRetainsFailure(t *testing.T) {
	calls := 0
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		calls++
		return `{"error":{"code":"catalog_unavailable","message":"cannot save machine"}}`, errors.New("exit status 1")
	}})
	_, err := client.SetMachineEnabled(context.Background(), "m1", false)
	var upstream *CommandError
	if !errors.As(err, &upstream) || upstream.Code != "catalog_unavailable" {
		t.Fatalf("upstream failure lost: %v", err)
	}
	for _, id := range []string{"", " m1", "m1 ", "--help"} {
		if _, err := client.SetMachineEnabled(context.Background(), id, false); err == nil {
			t.Fatalf("invalid profile ID accepted: %q", id)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid profile ID reached herdr: %d calls", calls)
	}
}
