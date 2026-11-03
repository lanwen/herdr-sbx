package panel

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGuestVersionBadgeDistinguishesLastKnownObservation(t *testing.T) {
	for _, mode := range []string{"current", "failed", "stale", "stopped", "expired"} {
		t.Run(mode, func(t *testing.T) {
			m := modelFixture(t)
			row := m.rows[0]
			row.Status.GuestServer = &store.GuestServerObservation{Version: "0.9.3", Current: true, CheckedAt: time.Now()}
			switch mode {
			case "failed":
				row.Status.Connection = "connection failed"
			case "stale":
				row.Status.Stale = true
			case "stopped":
				row.Sandbox.Status = "stopped"
			case "expired":
				row.Status.GuestServer.CheckedAt = time.Now().Add(-time.Hour)
			}
			label := m.guestServerLabel(row)
			if mode == "current" {
				require.NotContains(t, label, "last known")
			} else {
				require.Contains(t, label, "last known")
			}
			require.Contains(t, label, "0.9.3")
		})
	}
}

func TestGlobalVersionPrerequisiteNamesLocalCLI(t *testing.T) {
	rows := statusPrerequisites([]precheck.Check{{Name: "Herdr", Level: precheck.Ready, Detail: "herdr 0.9.3"}})
	require.Equal(t, "Local Herdr CLI", rows[0].Name)
}

func TestCaptureVersionProvenanceFrames(t *testing.T) {
	dir := os.Getenv("PANEL_VERSION_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set PANEL_VERSION_SCREENSHOT_DIR to capture version frames")
	}
	unsetNoColor(t)
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, width := range []int{110, 40} {
		for _, current := range []bool{true, false} {
			m := versionScreenshot(width, current)
			name := "guest-last-known-"
			if current {
				name = "guest-current-"
			}
			path := filepath.Join(dir, name+strconv.Itoa(width)+".ansi")
			require.NoError(t, os.WriteFile(path, []byte(m.View().Content), 0644))
		}
		m := versionScreenshot(width, true)
		m.openTab("Status")
		path := filepath.Join(dir, "local-cli-"+strconv.Itoa(width)+".ansi")
		require.NoError(t, os.WriteFile(path, []byte(m.View().Content), 0644))
	}
}

func versionScreenshot(width int, current bool) *model {
	m := screenshotModel()
	m.width, m.height = width, 42
	hostVersionFixture(m, "0.9.3")
	if !current {
		m.rows[0].Status.Connection = "connection failed"
		m.rows[0].Status.Detail = "A new SSH connection failed; the previous guest server observation was retained."
	}
	m.rows[0].Status.GuestServer.Current = current
	m.focus = "actions"
	for i, action := range m.sandboxActions(m.rows[0]) {
		if action.id == "fix-herdr" {
			m.button = i
		}
	}
	return m
}
