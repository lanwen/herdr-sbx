package panel

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// TestCaptureScreenshots is an opt-in capture fixture, not a live integration.
// It never initializes a service, reads saved state, or executes a command.
func TestCaptureScreenshots(t *testing.T) {
	dir := os.Getenv("PANEL_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set PANEL_SCREENSHOT_DIR to capture simulated frames")
	}
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, m *model) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(m.View().Content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	m := screenshotModel()
	write("sandboxes", m)
	m = screenshotModel()
	m.width, m.height = 110, 40
	m.expanded, m.focus = "claude-1", "actions"
	m.button = 2
	at := time.Date(2026, 10, 19, 12, 30, 0, 0, time.UTC)
	m.errors = []uiError{{ref: "claude", code: store.CodeGuestPluginSync, scope: store.StatusKey("claude", "claude-1"), pluginID: "claude-2", detail: "Manual intervention required. The herdr plugin build could not finish.", recovery: "Repair the build dependencies in this sandbox, then choose Re-sync to check the plugin again.", output: "Build step 1 failed: a required build dependency is unavailable.", count: 3, firstSeen: at, seen: at}}
	write("sandbox-details", m)
	m = screenshotModel()
	m.page, m.pluginSelected = "Plugins", "claude-1"
	m.height = 16
	write("plugins", m)
	m = screenshotModel()
	m.width, m.height = 40, 20
	write("compact", m)
}

func screenshotModel() *model {
	m := &model{page: "Sandboxes", focus: "table", width: 110, height: 18,
		queries: map[string]string{}, pending: map[string]bool{}, pendingChoices: map[string]pluginChoiceChange{},
		selected: "claude-1", workerEnabled: true, workerLoaded: true,
		worker:   store.WorkerStatus{State: "running", LastSync: time.Now()},
		plugins:  []guestplugins.Choice{{ID: "claude-1", Enabled: true, Installed: true}, {ID: "claude-2", Installed: true}, {ID: "claude-3", Installed: true, UnsupportedReason: "This herdr plugin does not support Linux sandboxes."}},
		settings: store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"claude-1": {Enabled: true}, "claude-2": {Enabled: false}}},
		checks:   precheck.Result{Checks: []precheck.Check{{Name: "herdr", Level: precheck.Ready, Detail: "herdr 0.9.0"}, {Name: "Sandbox daemon", Level: precheck.Ready, Detail: "Running"}, {Name: "Sandbox SSH", Level: precheck.Ready, Detail: "Configured"}}},
	}
	choices := []store.ManagementChoice{store.Watched, store.Watched, store.Unmanaged, store.Unwatched, store.Disabled}
	states := []string{"running", "stopped", "running", "running", "stopped"}
	connections := []string{"ready", "stopped", "ownership conflict", "ready", "stopped"}
	for i, name := range []string{"claude-1", "claude-2", "claude-3", "claude-4", "claude-5"} {
		row := registration.SandboxRow{Sandbox: sbx.Sandbox{ID: name, Name: name, Status: states[i]}, Scope: "claude", Management: choices[i], Label: name, Owned: i < 2,
			Status: store.SandboxStatus{Connection: connections[i], ObservedAt: time.Now(), GuestServer: &store.GuestServerObservation{Version: "0.9.0", Current: true, CheckedAt: time.Now()}}}
		if i == 0 {
			row.ProfileID = name
			row.Status.Detail = "This sandbox is registered and ready."
		}
		m.rows = append(m.rows, row)
	}
	return m
}
