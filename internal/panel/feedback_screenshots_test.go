package panel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCaptureEnableFeedback(t *testing.T) {
	dir := os.Getenv("PANEL_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set PANEL_SCREENSHOT_DIR for simulated frames")
	}
	unsetNoColor(t)
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, size := range [][2]int{{110, 32}, {110, 18}, {40, 12}, {30, 10}} {
		m := enableFeedbackScreenshotModel()
		m.width, m.height = size[0], size[1]
		require.NotNil(t, m.key(keyMsg(tea.KeyEnter, "")))
		writeFeedbackScreenshot(t, dir, "pending", m)
		m.Update(operationMsg{target: m.selected, code: store.CodeManagement, err: errors.New("Sandbox SSH proxy is unavailable. The plugin will check again automatically.")})
		writeFeedbackScreenshot(t, dir, "failed", m)
	}
	for _, size := range [][2]int{{110, 18}, {40, 12}} {
		m := screenshotModel()
		m.width, m.height = size[0], size[1]
		m.focus, m.dialog, m.dialogTarget, m.dialogReason, m.button = "actions", "fix-herdr", m.selected, "0.9.5", 0
		m.reveal = true
		writeInspectionScreenshot(t, dir, "confirmation", m)
		m = screenshotModel()
		m.width, m.height = size[0], size[1]
		m.focus, m.eventsExpanded, m.eventIndex, m.reveal = "events", true, 0, true
		m.events = []store.EventRecord{{Ref: "a1b2c3", Code: store.CodePreparation, Scope: store.StatusKey("examples", m.selected), Name: m.rows[0].Sandbox.Name, Detail: "Saved connection verification", Output: "stage=remote_probe\nidentity=verified\nresult=ready\nEND_OF_EVIDENCE", Resolved: true, At: time.Now()}}
		m.View()
		m.key(keyMsg(tea.KeyPgDown, ""))
		writeInspectionScreenshot(t, dir, "history", m)
	}

}

func enableFeedbackScreenshotModel() *model {
	m := screenshotModel()
	m.popup, m.refreshing = true, true
	m.focus, m.button = "actions", 0
	row := &m.rows[0]
	row.Sandbox.Name, row.Sandbox.Status = "claude-kubernetes-mcp-server", "stopped"
	row.EntryPresent, row.CreationRequested = false, false
	row.Label, row.ProfileID = "", ""
	row.Enabled, row.NativeEnabled = store.Disabled, nil
	row.Status.Connection, row.Status.Detail = "stopped", ""
	row.Status.GuestServer = nil
	for i := range 16 {
		extra := m.rows[4]
		extra.Sandbox.ID = fmt.Sprintf("%08x-8151-4ca2-a2dd-ef04e0995857", i+1)
		extra.Sandbox.Name = fmt.Sprintf("codex-project-%02d", i)
		m.rows = append(m.rows, extra)
	}
	m.events = []store.EventRecord{{Ref: "a1b2c3", Code: store.CodePreparation, Scope: store.StatusKey("examples", m.selected), Name: row.Sandbox.Name, Detail: "Checked the saved sandbox connection", At: time.Now()}}
	return m
}

func writeFeedbackScreenshot(t *testing.T, dir, state string, m *model) {
	t.Helper()
	frame := m.View().Content
	require.Len(t, strings.Split(frame, "\n"), m.height)
	require.Contains(t, ansi.Strip(frame), "Events")
	if state == "failed" {
		require.Contains(t, ansi.Strip(frame), store.CodeManagement)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("enable-%s-%dx%d.ansi", state, m.width, m.height)), []byte(frame), 0644))
}

func writeInspectionScreenshot(t *testing.T, dir, state string, m *model) {
	t.Helper()
	frame := m.View().Content
	require.Len(t, strings.Split(frame, "\n"), m.height)
	require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("enable-%s-%dx%d.ansi", state, m.width, m.height)), []byte(frame), 0644))
}
