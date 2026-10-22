package panel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func navigationErrors() []uiError {
	return []uiError{{code: store.CodeRename, scope: "a", detail: "first error"}, {code: store.CodePreparation, scope: "b", detail: "second error"}}
}

func TestSandboxArrowsTraverseTableAndErrors(t *testing.T) {
	for _, keys := range []struct{ down, up tea.KeyPressMsg }{
		{keyMsg(tea.KeyDown, ""), keyMsg(tea.KeyUp, "")},
		{keyMsg('j', "j"), keyMsg('k', "k")},
	} {
		m := modelFixture(t)
		m.errors = navigationErrors()
		m.selected, m.expanded = "b", "b" // Diagnostics retain the selected preview.
		m.key(keys.down)
		require.Equal(t, "diagnostics", m.focus, "Down skipped diagnostics header")
		m.key(keys.down)
		require.EqualValues(t, "errors", m.focus)
		require.EqualValues(t, 0, m.errorIndex)
		require.True(t, m.reveal)
		require.EqualValues(t, "b", m.expanded)
		m.key(keys.down)
		require.Equal(t, 1, m.errorIndex, "Down did not select the next error")
		m.key(keys.up)
		require.EqualValues(t, "errors", m.focus)
		require.EqualValues(t, 0, m.errorIndex)
		m.key(keys.up)
		require.Equal(t, "diagnostics", m.focus, "Up skipped diagnostics header")
		m.key(keys.up)
		require.EqualValues(t, "table", m.focus)
		require.EqualValues(t, "b", m.selected)
		require.EqualValues(t, "b", m.expanded)
		require.True(t, m.reveal)
	}
}

func TestFocusedDetailsStayInsideUntilEscapeThenDiagnosticsReturnToPreview(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.errors = navigationErrors()
	m.plugins = []guestplugins.Choice{{ID: "first.plugin"}, {ID: "second.plugin"}}
	m.selected, m.expanded = "b", "b"
	m.rows[1].Enabled, m.rows[1].EntryPresent = store.Enabled, true
	m.rows[1].Status.Connection = "ready"
	m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, 0, m.button)
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "plugins", m.focus)
	require.EqualValues(t, 0, m.detailIndex)
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "plugins", m.focus)
	require.EqualValues(t, 1, m.detailIndex)
	require.EqualValues(t, "b", m.selected)
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "b", m.expanded)
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "diagnostics", m.focus, "list Down skipped diagnostic header")
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, 0, m.errorIndex)
	require.EqualValues(t, "b", m.expanded)

	content := ansi.Strip(m.View().Content)
	require.Contains(t, content, "> H0002 · alpha", "first error not revealed below details: %s", content)

	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	m.View()
	for _, h := range m.hits {
		if h.action == "select" && h.id == "b" {
			return
		}
	}
	t.Fatal("returning from diagnostics did not reveal sandbox preview")
}

func TestEmptyFilteredTableCanReachErrorsWithoutClearingFilter(t *testing.T) {
	m := modelFixture(t)
	m.queries["Sandboxes"] = "no match"
	m.errors = navigationErrors()
	m.ensureSelection()
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "diagnostics", m.focus, "Down skipped diagnostics header")
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, 0, m.errorIndex)
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "no match", m.queries["Sandboxes"])
	require.Len(t, m.visibleRows(), 0)
	require.EqualValues(t, 0, m.scroll)
}

func TestBoundaryNavigationDoesNotCaptureInputsOrDialogs(t *testing.T) {
	for _, context := range []string{"no errors", "filter", "dialog"} {
		t.Run(context, func(t *testing.T) {
			m := modelFixture(t)
			m.selected = "b"
			if context != "no errors" {
				m.errors = navigationErrors()
			}
			if context == "filter" {
				m.input = &inputState{kind: "filter", text: "draft", cursor: 2}
			} else if context == "dialog" {
				m.dialog = "startup"
			}
			m.key(keyMsg(tea.KeyDown, ""))
			require.EqualValues(t, "table", m.focus)
			require.False(t, context != "filter" && m.selected != "b")
			require.False(t, context == "filter" && (m.input == nil || m.input.text != "draft" || m.input.cursor != 2), "Down changed the active filter")
			require.False(t, context == "dialog" && (m.dialog != "startup" || m.button != 0), "Down did not retain dialog-local navigation")
		})
	}
}

func TestOtherTabsTraverseTheirFinalControlAndErrors(t *testing.T) {
	for _, page := range []string{"Plugins", "Status"} {
		m := modelFixture(t)
		m.page = page
		m.errors = navigationErrors()
		m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
		m.key(keyMsg(tea.KeyEnd, ""))
		lastButton := m.button
		m.key(keyMsg(tea.KeyDown, ""))
		m.key(keyMsg(tea.KeyDown, ""))
		require.EqualValues(t, "errors", m.focus)
		require.EqualValues(t, 0, m.errorIndex)
		m.key(keyMsg(tea.KeyUp, ""))
		m.key(keyMsg(tea.KeyUp, ""))
		require.EqualValues(t, "table", m.focus)
		require.False(t, page == "Status" && m.button != lastButton)
		require.False(t, page == "Plugins" && m.pluginSelected != "two")
	}
}

func TestErrorShortcutAndPagingRetainTheirBehavior(t *testing.T) {
	m := modelFixture(t)
	m.errors = navigationErrors()
	m.key(keyMsg('e', "e"))
	require.Equal(t, "errors", m.focus, "e no longer focuses Errors")
	m.key(keyMsg(tea.KeyHome, ""))
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, 0, m.errorIndex)
	m.key(keyMsg(tea.KeyPgUp, ""))
	require.Equal(t, "errors", m.focus, "Page Up left Errors")
	m.errorIndex, m.scroll = len(m.errors)-1, 3
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "errors", m.focus)
	require.False(t, m.errorIndex != len(m.errors)-1)
	require.EqualValues(t, 4, m.scroll)
}

func TestResolvedLastErrorReturnsFocusToTable(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.errors = navigationErrors()
	m.focusErrors()
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}})
	require.Len(t, m.errors, 0)
	require.EqualValues(t, "table", m.focus)
	require.True(t, m.reveal)
	require.EqualValues(t, 0, m.scroll)
}
