package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
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
			require.NoError(t, err)
			require.Len(t, machines, 1)
			got := machines[0].Enabled
			require.False(t, (got != nil) != tc.known)
			require.False(t, got != nil && *got != tc.want)
		})
	}
	{
		_, err := parseMachines(`[{"id":"m1","enabled":"false"}]`)
		require.Error(t, err, "non-boolean native state was accepted")
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
				require.EqualValues(t, "herdr", command.Name)
				require.False(t, strings.Join(command.Args, " ") != "machine "+action+" m1")
				require.EqualValues(t, "isolated.sock", command.Env["HERDR_SOCKET_PATH"])
				return "saved", nil
			}})
			client.RebindSocket("isolated.sock")
			output, err := client.SetMachineEnabled(context.Background(), "m1", enabled)
			require.NoError(t, err)
			require.EqualValues(t, "saved", output)
			require.EqualValues(t, 1, calls)
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
	require.ErrorAs(t, err, &upstream)
	require.EqualValues(t, "catalog_unavailable", upstream.Code)
	for _, id := range []string{"", " m1", "m1 ", "--help"} {
		{
			_, err := client.SetMachineEnabled(context.Background(), id, false)
			require.Error(t, err, "invalid profile ID accepted: %q", id)
		}
	}
	require.Equal(t, 1, calls, "invalid profile ID reached herdr: %d calls", calls)
}
