package herdr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestGuestHandoffRequiresVerifiedServerWithoutRestartFallback(t *testing.T) {
	for _, scenario := range []string{"success", "rejected", "wrong version", "not running"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
				calls++
				script := command.Args[len(command.Args)-1]
				if command.Name != "ssh" || command.Args[4] != "fixture.sbx" {
					t.Fatalf("wrong target: %+v", command)
				}
				switch {
				case strings.Contains(script, "server live-handoff"):
					if !strings.Contains(script, `--import-exe "$h" --expected-version '0.9.3'`) {
						t.Fatal(script)
					}
					if scenario == "rejected" {
						return "unsupported", errors.New("exit 1")
					}
					return "live handoff complete", nil
				case strings.HasSuffix(script, "status server --json"):
					if scenario == "wrong version" {
						return `{"running":true,"version":"0.9.4"}`, nil
					}
					if scenario == "not running" {
						return `{"running":false}`, nil
					}
					return `{"result":{"running":true,"version":"0.9.3"}}`, nil
				default:
					t.Fatalf("unexpected recovery command: %s", script)
					return "", nil
				}
			}})
			err := client.HandoffGuest(context.Background(), "fixture.sbx", "0.9.3")
			if (err == nil) != (scenario == "success") {
				t.Fatalf("error=%v", err)
			}
			if scenario == "rejected" && calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
