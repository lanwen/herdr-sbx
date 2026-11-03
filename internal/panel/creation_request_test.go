package panel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStoppedCreationRequestCanBeDisabledFromPanel(t *testing.T) {
	ui := testPanel(t, nil)
	scope := store.ScopeID("/tmp/panel-test.sock")
	accepted, err := store.RequestCreation(scope, "stopped", "aaa")
	require.NoError(t, err)
	require.True(t, accepted)
	rows, err := ui.registration.SandboxRows(context.Background(), "/tmp/panel-test.sock")
	require.NoError(t, err)
	m := newModel(ui)
	m.loading, m.refreshing = false, true
	m.rows, m.selected = rows, "stopped"
	row, ok := m.row()
	require.True(t, ok)
	require.False(t, row.EntryPresent)
	require.Equal(t, store.Enabled, row.Enabled)
	require.Equal(t, "waiting for provisioning", row.Status.Connection)
	require.Equal(t, "disable", m.sandboxActions(row)[0].id, "pending creation needs an explicit cancellation action")
	require.Contains(t, ansi.Strip(m.renderHelp(110, false)), "space disable")
	m.focus, m.expanded = "actions", m.selected
	hint, _ := m.contextHint()
	require.Equal(t, "Cancel automatic initial registration for this sandbox. Existing files stay in place.", hint)
	m.focus = "table"
	command := m.key(keyMsg(tea.KeySpace, " "))
	require.NotNil(t, command)
	m.Update(command())
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Empty(t, status.CreationRequests)
	require.Equal(t, store.Disabled, status.EnabledChoices[store.StatusKey(scope, "stopped")])
}

func TestCaptureCreatedRegistrationRequest(t *testing.T) {
	dir := os.Getenv("PANEL_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set PANEL_SCREENSHOT_DIR to capture simulated frames")
	}
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, width := range []int{110, 40} {
		m := screenshotModel()
		m.width, m.height = width, 42
		m.focus, m.button = "actions", 0
		row := &m.rows[0]
		row.Sandbox.Status = "stopped"
		row.EntryPresent, row.CreationRequested = false, true
		row.Label, row.ProfileID = "", ""
		row.Status.Connection = "waiting for provisioning"
		row.Status.Detail = "Initial registration is pending. The plugin will retry automatically when this sandbox is ready."
		row.Status.GuestServer = nil
		frame := m.View().Content
		require.Contains(t, ansi.Strip(frame), "Disable")
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("creation-pending-%d.ansi", width)), []byte(frame), 0644))
	}
}
