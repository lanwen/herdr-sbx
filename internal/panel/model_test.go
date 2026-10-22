package panel

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func keyMsg(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }
func clickAt(m *model, x, y int) tea.Cmd {
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, command := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	return command
}

func modelFixture(t *testing.T) *model {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m := newModel(nil)
	m.loading = false
	m.workerEnabled = false
	disabled := false
	m.rows = []registration.SandboxRow{{Sandbox: sbx.Sandbox{ID: "a", Name: "alpha", Status: "running"}, Scope: "scope", Enabled: store.Enabled, EntryPresent: true, Label: "API desk"}, {Sandbox: sbx.Sandbox{ID: "b", Name: "beta", Status: "stopped"}, Scope: "scope", Enabled: store.Disabled, EntryPresent: true, NativeEnabled: &disabled, Label: "Release desk"}}
	m.ensureSelection()
	return m
}
func TestExpansionSelectionAndFiltering(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnter, ""))
	require.Equal(t, "a", m.expanded, m.expanded)
	m.key(keyMsg(tea.KeyEscape, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyEnter, ""))
	require.Equal(t, "b", m.expanded, m.expanded)
	m.key(keyMsg('/', "/"))
	m.key(keyMsg('A', "API"))
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	require.Len(t, m.visibleRows(), 1)
	m.key(keyMsg(tea.KeyEscape, ""))
	require.Equal(t, "API", m.queries["Sandboxes"], "Escape discarded filter")
	m.key(keyMsg(tea.KeyEscape, ""))
	m.key(keyMsg(tea.KeyEscape, ""))
	require.Equal(t, "", m.queries["Sandboxes"], "table Escape failed to clear")
}
func TestRenamePreservesTextAcrossRefreshAndFailure(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.input.text = "New desk"
	m.input.cursor = 8
	m.mergeRows([]registration.SandboxRow{m.rows[1], m.rows[0]})
	require.EqualValues(t, "a", m.rows[0].Sandbox.ID)
	require.EqualValues(t, "New desk", m.input.text)
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("name taken"), rename: true})
	require.NotNil(t, m.input)
	require.EqualValues(t, "New desk", m.input.text)
	m.key(keyMsg(tea.KeyEscape, ""))
	require.Equal(t, "API desk", m.rows[0].Label, "cancel changed label")
}
func TestInputCapturesGlobalShortcuts(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.key(keyMsg('q', "q"))
	m.key(keyMsg('p', "p"))
	require.EqualValues(t, "Sandboxes", m.page)
	require.True(t, strings.HasSuffix(m.input.text, "qp"))
}
func TestInventoryFailureRetainsRowsAndPersistsError(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	_, cmd := m.Update(observationMsg{err: errors.New("daemon unavailable")})
	runCommands(cmd)
	require.Len(t, m.rows, 2)
	require.True(t, m.rows[0].Status.Stale)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 1)
}
func TestStoppedWorkerPromptDefaultsToKeepStopped(t *testing.T) {
	m := modelFixture(t)
	_, cmd := m.Update(workerMsg{initialized: true, status: store.WorkerStatus{State: "stopped"}})
	require.Nil(t, cmd)
	require.EqualValues(t, "startup", m.dialog)
	require.EqualValues(t, 0, m.button)
	m.key(keyMsg(tea.KeyEscape, ""))
	require.Equal(t, "", m.dialog, "decline failed")
	m.key(keyMsg('p', "p"))
	require.Equal(t, "Plugins", m.page, "configuration unavailable while stopped")
}
func TestSmallViewPreservesWorkerErrorsAndState(t *testing.T) {
	for _, mode := range []string{"colored", "plain"} {
		t.Run(mode, func(t *testing.T) {
			unsetNoColor(t)
			if mode == "plain" {
				t.Setenv("NO_COLOR", "1")
			}
			m := modelFixture(t)
			m.width, m.height = 60, 18
			m.selected, m.expanded = "b", "b"
			m.addError(store.CodeSSHSetup, "Host", errors.New("missing configuration"))
			view := m.View()
			visible := ansi.Strip(view.Content)
			for _, want := range []string{"Management OFF", "beta (stopped)", "Release desk", "? help"} {
				require.Contains(t, visible, want, "missing %q in %s", want, visible)
			}
			m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
			view = m.View()
			require.NotContains(t, ansi.Strip(view.Content), "Resize")
			require.EqualValues(t, "b", m.selected)
			require.EqualValues(t, "b", m.expanded)
		})
	}
}

func TestPluginOverrideShowsLiveDefaultAndSavedChoice(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", Enabled: true}}
	m.settings = store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"test.plugin": {Enabled: true}}, Overrides: map[string]map[string]bool{store.StatusKey("scope", "a"): {"test.plugin": false}}}
	m.expanded = "a"
	view := m.View()
	require.Contains(t, view.Content, "Off")
	require.Contains(t, view.Content, "test.plugin")
}

func TestArrowSelectionKeepsPreviewWithSelection(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnd, ""))
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "table", m.focus)
	m.ensureSelection()
	require.Equal(t, "b", m.expanded, "refresh lost preview")
	m.hitAction(hit{action: "select", id: "a"})
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "a", m.expanded)
	require.EqualValues(t, "table", m.focus)
	m.focus = "actions"
	m.mergeRows(m.rows[1:])
	m.ensureSelection()
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "table", m.focus)
}
func TestMouseCannotNavigateAwayFromActiveInput(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.hitAction(hit{action: "select", id: "b"})
	m.hitAction(hit{action: "page", id: "Plugins"})
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "Sandboxes", m.page)
	require.NotNil(t, m.input)
}
func TestOldRenameCompletionDoesNotEraseNewInput(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	old := m.input
	m.input = nil
	m.beginRename()
	current := m.input
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, rename: true, input: old})
	require.False(t, m.input != current, "old operation erased new input")
}
func TestCloseWaitsForPendingCleanup(t *testing.T) {
	m := modelFixture(t)
	m.pending["a"] = true
	cmd := m.key(keyMsg('q', "q"))
	require.Nil(t, cmd)
	require.True(t, m.closing)
	_, cmd = m.Update(operationMsg{target: "a", code: store.CodeTemporaryAccess})
	require.NotNil(t, cmd, "did not close after cleanup")
}

func runCommands(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runCommands(child)
		}
	}
}

func TestRemovalErrorRecoverySelectsDisable(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeManagement, scope: "a", detail: "entry remains"}}
	m.focusErrors()
	m.activate()
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, "disable", m.sandboxActions(m.rows[0])[m.button].id)
}
func TestNarrowActiveInputsRemainVisible(t *testing.T) {
	for _, kind := range []string{"rename", "filter"} {
		t.Run(kind, func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = 60, 18
			m.expanded = "a"
			m.plugins = make([]guestplugins.Choice, 30)
			m.input = &inputState{kind: kind, target: "a", text: "editable", cursor: 8}
			view := m.View()
			caret := "│"
			if kind == "rename" {
				caret = "█"
			}
			require.Contains(t, view.Content, "editable"+caret, "input hidden: %s", view.Content)
		})
	}
}
func TestMouseRenameRevealsInlineEditor(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.expanded = "a"
	m.plugins = make([]guestplugins.Choice, 30)
	m.focus = "actions"
	m.hitAction(hit{action: "action", id: "rename"})
	view := m.View()
	require.NotNil(t, m.input)
	require.Contains(t, view.Content, "API desk█")
}
func TestErrorKeyboardSelectionRevealsRecoveryTarget(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	for i := 0; i < 10; i++ {
		m.errors = append(m.errors, uiError{code: store.CodeRename, scope: string(rune('a' + i)), detail: strings.Repeat("long diagnostic ", 8)})
	}
	m.focusErrors()
	for i := 0; i < 8; i++ {
		m.key(keyMsg(tea.KeyDown, ""))
	}
	view := m.View()
	require.Contains(t, ansi.Strip(view.Content), "> H0002 · i", "selected error hidden: %s", view.Content)
}

func TestPanelObservationWhileOffDoesNotScheduleWork(t *testing.T) {
	m := modelFixture(t)
	m.worker = store.WorkerStatus{State: "stopped"}
	m.workerLoaded = true
	_, cmd := m.Update(observationMsg{checks: precheck.Result{Scope: "scope", Socket: "/tmp/test.sock"}})
	require.Nil(t, cmd)
	require.Len(t, m.pending, 0)
}

func TestPollingPreservesFocusedActionAndPreview(t *testing.T) {
	m := modelFixture(t)
	m.focus = "actions"
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "rename" {
			m.button = i
		}
	}
	fresh := m.rows[0]
	fresh.Status.Connection = "ready"
	m.mergeRows([]registration.SandboxRow{fresh, m.rows[1]})
	m.ensureSelection()
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, "rename", m.sandboxActions(m.rows[0])[m.button].id)
	require.False(t, m.expanded != m.selected)
	m.page, m.focus, m.button = "Status", "table", len(m.statusActions())-1
	last := m.button
	m.ensureSelection()
	require.Equal(t, last, m.button, "sandbox poll changed Status action")
}

func TestRenameSubmissionSkipsUnchangedLabel(t *testing.T) {
	for _, test := range []struct {
		name, text string
		changed    bool
	}{
		{"unchanged", "API desk", false},
		{"surrounding whitespace", "  API desk  ", false},
		{"changed", "New desk", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := modelFixture(t)
			m.beginRename()
			m.input.text = test.text
			cmd := m.inputKey(keyMsg(tea.KeyEnter, ""))
			require.False(t, (cmd != nil) != test.changed)
			require.False(t, m.pending["a"] != test.changed)
			require.False(t, (m.input != nil) != test.changed)
			require.Equal(t, "API desk", m.rows[0].Label, "submission replaced observed label before a confirmed change")
		})
	}
}
