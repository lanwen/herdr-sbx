package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestGuestHandoffRequiresVerifiedServerWithoutRestartFallback(t *testing.T) {
	for _, scenario := range []string{"success", "rejected", "wrong version", "not running"} {
		t.Run(scenario, func(t *testing.T) {
			testGuestHandoffScenario(t, scenario)
		})
	}
}

func testGuestHandoffScenario(t *testing.T, scenario string) {

	fixture := handoffScenarioFixture{t: t, scenario: scenario}
	client := New(process.Runner{Execute: fixture.run})

	err := client.HandoffGuest(context.Background(), "fixture.sbx", "0.9.3")
	require.Equal(t, (scenario == "success"), (err == nil), "error=%v", err)
	require.False(t, scenario == "rejected" && fixture.calls != 1, "calls=%d", fixture.calls)

}

type handoffScenarioFixture struct {
	t        *testing.T
	scenario string
	calls    int
}

func (f *handoffScenarioFixture) run(_ context.Context, command process.Command) (string, error) {

	f.calls++
	script := command.Args[len(command.Args)-1]
	require.EqualValues(f.t, "ssh", command.Name)
	require.EqualValues(f.t, "fixture.sbx", command.Args[4])
	switch {
	case strings.Contains(script, "server live-handoff"):
		require.Contains(f.t, script, `--import-exe "$h" --expected-version '0.9.3'`, script)
		if f.scenario == "rejected" {
			return "unsupported", errors.New("exit 1")
		}
		return "live handoff complete", nil
	case strings.HasSuffix(script, "status server --json"):
		if f.scenario == "wrong version" {
			return `{"running":true,"version":"0.9.4"}`, nil
		}
		if f.scenario == "not running" {
			return `{"running":false}`, nil
		}
		return `{"running":true,"version":"0.9.3"}`, nil
	default:
		f.t.Fatalf("unexpected recovery command: %s", script)
		return "", nil
	}
}
