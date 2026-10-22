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
	m.width, m.height = 110, 42
	m.expanded, m.focus = m.selected, "actions"
	m.button = 1
	at := time.Date(2026, 10, 19, 12, 30, 0, 0, time.UTC)
	m.errors = []uiError{{ref: "claude", code: store.CodeGuestPluginSync, scope: store.StatusKey("claude", m.selected), pluginID: "claude-2", detail: "Manual intervention required. The herdr plugin build could not finish.", recovery: "Repair the build dependencies in this sandbox, then choose Re-sync to check the plugin again.", output: "Build step 1 failed: a required build dependency is unavailable.", count: 3, firstSeen: at, seen: at}}
	write("sandbox-details", m)
	m = screenshotModel()
	m.width, m.height = 110, 28
	m.focus, m.button = "actions", 0
	m.helpOpen = true
	write("help", m)
	m = screenshotModel()
	m.page, m.width, m.height = "Status", 110, 28
	m.worker.Detail = "Following sandbox lifecycle events."
	m.checks.Checks = []precheck.Check{
		{Name: "Herdr", Level: precheck.Ready, Detail: "herdr 0.9.3"},
		{Name: "sbx", Level: precheck.Ready, Detail: "sbx 0.7.0"},
		{Name: "Sandbox capability", Level: precheck.Ready, Detail: "JSON daemon status and inventory are available"},
		{Name: "OpenSSH", Level: precheck.Ready, Detail: "OpenSSH_10.0p2"},
		{Name: "Sandbox daemon", Level: precheck.Ready, Detail: "running"},
		{Name: "Sandbox inventory", Level: precheck.Ready, Detail: "local sandbox inventory is readable"},
		{Name: "Docker authorization", Level: precheck.Ready, Detail: "Docker sign-in verified"},
		{Name: "Sandbox SSH", Level: precheck.Ready, Detail: "managed sandbox SSH proxy is configured"},
	}
	write("status", m)
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
		workerEnabled: true, workerLoaded: true,
		worker:   store.WorkerStatus{State: "running", LastSync: time.Now()},
		plugins:  []guestplugins.Choice{{ID: "claude-1", Enabled: true, Installed: true}, {ID: "claude-2", Installed: true}, {ID: "claude-3", Installed: true, UnsupportedReason: "This herdr plugin does not support Linux sandboxes."}},
		settings: store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"claude-1": {Enabled: true}, "claude-2": {Enabled: false}}},
		checks:   precheck.Result{Checks: []precheck.Check{{Name: "herdr", Level: precheck.Ready, Detail: "herdr 0.9.0"}, {Name: "Sandbox daemon", Level: precheck.Ready, Detail: "Running"}, {Name: "Sandbox SSH", Level: precheck.Ready, Detail: "Configured"}}},
	}
	choices := []store.EnabledChoice{store.Enabled, store.Enabled, store.Disabled, store.Disabled, store.Disabled}
	states := []string{"running", "stopped", "running", "stopped", "stopped"}
	connections := []string{"Ready", "Waiting for start", "Disabled in herdr", "Stopped", "Stopped"}
	sandboxIDs := []string{
		"bbfd38ff-8151-4ca2-a2dd-ef04e0995857",
		"178a804c-01a8-4b56-a98a-c0195c6dc27d",
		"45869cb2-429e-45ba-b714-79daa736d586",
		"d310d4db-b16a-4a09-a831-fec0a7ea6ad8",
		"e8321142-7f39-4b84-9ac8-11fa7080d9fb",
	}
	profileIDs := []string{"25a9b0f5f63eab13de0b68573e18cd92", "92c3df06bd5143e7a76d1f608d841b2c", "4cd97acb00af4ce68239298e9f082936"}
	for i, name := range []string{"claude-1", "claude-2", "claude-3", "claude-4", "claude-5"} {
		row := registration.SandboxRow{Sandbox: sbx.Sandbox{ID: sandboxIDs[i], Name: name, Status: states[i]}, Scope: "claude", Enabled: choices[i], Label: name, EntryPresent: i < 3,
			Status: store.SandboxStatus{Connection: connections[i], ObservedAt: time.Now(), GuestServer: &store.GuestServerObservation{Version: "0.9.0", Current: true, CheckedAt: time.Now()}}}
		native := i == 0
		if row.EntryPresent {
			row.NativeEnabled = &native
			row.ProfileID = profileIDs[i]
		} else {
			row.Label = ""
		}
		if i == 0 {
			row.Status.Detail = "This sandbox is registered in herdr. Setup is complete."
		}
		m.rows = append(m.rows, row)
	}
	m.selected, m.expanded = sandboxIDs[0], sandboxIDs[0]
	return m
}
