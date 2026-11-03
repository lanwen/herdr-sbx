package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCreatedStoppedSandboxIsAdded(t *testing.T) {
	for _, priorObservation := range []string{"fresh", "passive poll", "baseline"} {
		t.Run(priorObservation, func(t *testing.T) {
			f, adds, guestCalls := newCreatedFixture(t)
			if priorObservation == "passive poll" {
				_, err := f.service.registrations.SandboxRows(context.Background(), "/tmp/lifecycle-test.sock")
				require.NoError(t, err)
			}
			if priorObservation == "baseline" {
				require.NoError(t, f.service.registrations.CaptureBaseline(context.Background(), "/tmp/lifecycle-test.sock"))
			}
			scheduler := f.scheduler()
			scheduler.handle(createdSandboxEvent())
			require.Equal(t, 1, *adds, "created stopped sandbox was not added")
			saved, err := store.LoadState()
			require.NoError(t, err)
			require.Equal(t, "machine-1", saved.Scopes[f.scope][0].ProfileID)
			status, err := store.ReadStatusStore()
			require.NoError(t, err)
			require.Empty(t, status.Errors)
			before := *guestCalls
			f.request = func(*http.Request) (*http.Response, error) {
				return lifecycleResponse(200, `[{"uuid":"new","name":"demo","status":"stopped"}]`), nil
			}
			scheduler.handle(createdSandboxEvent())
			require.Equal(t, 1, *adds, "duplicate creation added another entry")
			require.Equal(t, before, *guestCalls, "duplicate creation connected to existing stopped entry")
		})
	}
}

func TestCreatedSandboxPreservesInactiveChoices(t *testing.T) {
	for _, mode := range []string{"Management OFF", "Disabled", "started", "replaced", "unknown runtime"} {
		t.Run(mode, func(t *testing.T) {
			f, adds, guestCalls := newCreatedFixture(t)
			event := createdSandboxEvent()
			switch mode {
			case "Management OFF":
				require.NoError(t, store.SetWorkerControl(true))
			case "Disabled":
				require.NoError(t, store.SetEnabled(f.scope, "new", store.Disabled))
			case "started":
				event.Action = "started"
			case "replaced":
				event.SandboxID = "old"
			case "unknown runtime":
				f.request = func(*http.Request) (*http.Response, error) {
					return lifecycleResponse(200, `[{"uuid":"new","name":"demo","status":"starting"}]`), nil
				}
			}
			f.scheduler().handle(event)
			require.Zero(t, *adds)
			require.Zero(t, *guestCalls)
		})
	}
}

func createdSandboxEvent() sbx.LifecycleEvent {
	return sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "created", SandboxID: "new", SandboxName: "demo"}
}

func newCreatedFixture(t *testing.T) (*lifecycleFixture, *int, *int) {
	t.Helper()
	f := newLifecycleFixture(t)
	f.catalog = `[]`
	runtime := "stopped"
	f.request = func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`[{"uuid":"new","name":"demo","status":%q}]`, runtime)
		if runtime == "starting" {
			runtime = "running"
		}
		return lifecycleResponse(200, body), nil
	}
	adds, guestCalls := 0, 0
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		joined := strings.Join(cmd.Args, " ")
		if cmd.Name == "herdr" && strings.HasPrefix(joined, "machine add ") {
			adds++
			require.Equal(t, "machine add --label sbx · demo demo.sbx", joined)
			raw, err := json.Marshal([]map[string]any{{"id": "machine-1", "label": "sbx · demo", "target": "demo.sbx", "session": "default", "enabled": true}})
			require.NoError(t, err)
			f.catalog = string(raw)
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		}
		if cmd.Name == "sbx" && len(cmd.Args) > 0 && cmd.Args[0] == "exec" {
			guestCalls++
			return "", nil
		}
		if cmd.Name == "ssh" && strings.Contains(joined, "demo.sbx") {
			guestCalls++
			return createdGuestResponse(joined, &runtime), nil
		}
		return f.defaultRun(ctx, cmd)
	}
	return f, &adds, &guestCalls
}

func createdGuestResponse(command string, runtime *string) string {
	if strings.Contains(command, "__herdr_on_path__") {
		*runtime = "starting"
		return "__herdr_on_path__\nherdr 0.9.1\n"
	}
	if strings.Contains(command, "api schema --json") {
		return `{"schemas":{"request":{"$defs":{"IntegrationTarget":{"enum":["claude"]}}}}}`
	}
	if strings.Contains(command, "status server --json") {
		return `{"running":true,"version":"0.9.1"}`
	}
	return ""
}
