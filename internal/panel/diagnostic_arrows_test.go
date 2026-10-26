package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticHeaderArrowsSelectNewAndAll(t *testing.T) {
	for _, page := range []string{"Sandboxes", "Status", "Plugins"} {
		t.Run(page, func(t *testing.T) {
			testDiagnosticHeaderArrows(t, page)
		})
	}
}

func TestDiagnosticHeaderRetainsTabCyclingAndActivation(t *testing.T) {
	m := modelFixture(t)
	m.focus = "diagnostics"
	m.key(keyMsg(tea.KeyTab, ""))
	require.EqualValues(t, "Status", m.page)
	require.EqualValues(t, "table", m.focus)
	m.focus = "diagnostics"
	m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "table", m.focus)
	m.focus = "diagnostics"
	m.key(keyMsg(tea.KeyEnter, ""))
	require.True(t, m.showAllErrors, "Enter did not toggle diagnostic view")
	m.key(keyMsg(tea.KeySpace, " "))
	require.False(t, m.showAllErrors, "Space did not toggle diagnostic view")
	m.hitAction(hit{action: "diagnostic-toggle"})
	require.True(t, m.showAllErrors, "mouse header did not toggle diagnostic view")
	m.focus = "errors"
	m.key(keyMsg(tea.KeyRight, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "errors", m.focus)
}

func testDiagnosticHeaderArrows(t *testing.T, page string) {
	t.Helper()
	unsetNoColor(t)
	m := modelFixture(t)
	m.page, m.focus = page, "diagnostics"
	m.errors = []uiError{
		{code: store.CodeGuestPluginSync, scope: "a", detail: "new failure", count: 2},
		{code: store.CodeOwnershipConflict, scope: "b", detail: "dismissed warning", count: 3, dismissed: true},
	}
	for _, step := range []struct {
		key rune
		all bool
	}{{tea.KeyLeft, false}, {tea.KeyRight, true}, {tea.KeyRight, true}, {tea.KeyLeft, false}, {tea.KeyLeft, false}} {
		m.key(keyMsg(step.key, ""))
		require.False(t, m.showAllErrors != step.all)
		require.EqualValues(t, "diagnostics", m.focus)
		require.False(t, m.page != page)
		wantVisible := 1
		if step.all {
			wantVisible = 2
		}
		require.Len(t, m.visibleErrors(), wantVisible, "visible groups=%d want %d", len(m.visibleErrors()), wantVisible)
		title := m.diagnosticTitle()
		require.Contains(t, ansi.Strip(title), "Errors and warnings · New (2) | All (5)")
		require.False(t, strings.Contains(title, "3 active hidden") == step.all)
	}
	for _, full := range []bool{false, true} {
		help := strings.Join(strings.Fields(ansi.Strip(m.renderHelp(110, full))), " ")
		require.Contains(t, help, "←/→ new/all")
		require.NotContains(t, help, "←/→ cycle tabs")
	}

}
