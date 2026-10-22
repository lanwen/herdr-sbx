package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDetailsConfirmationUsesHintWithLocalControls(t *testing.T) {
	m := modelFixture(t)
	m.expanded, m.selected, m.focus = "b", "b", "actions"
	m.dialog, m.dialogTarget, m.dialogReason = "fix-herdr", "b", "0.9.3"
	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	details := strings.Index(view, "beta (stopped)")
	confirm := strings.Index(view, "Fix herdr in beta?")
	require.False(t, details < 0)
	require.False(t, hint < details)
	require.False(t, confirm < hint)
	m.key(keyMsg(tea.KeyRight, ""))
	require.EqualValues(t, 1, m.button)
	require.EqualValues(t, "fix-herdr", m.dialog)
	require.EqualValues(t, "Sandboxes", m.page)
	m.View()
	for _, h := range m.hits {
		if h.action == "dialog" && h.id == "0" {
			require.False(t, h.y <= strings.Count(view[:hint], "\n"), "confirmation mouse target is outside Hint")
			clickAt(m, h.x, h.y)
			require.EqualValues(t, "", m.dialog)
			require.EqualValues(t, "b", m.expanded)
			return
		}
	}
	t.Fatal("Hint confirmation has no Cancel mouse target")
}

func TestSandboxActionSpinnerAndOutcomeUseHint(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.run("a", store.CodeManagement, "Saved management choice", func() error { return nil })
	first := ansi.Strip(m.View().Content)
	hint := strings.Index(first, "╭─ Hint")
	require.False(t, hint < 0)
	require.Contains(t, first[hint:], "◐ Updating sandbox connection for alpha")
	m.ticks++
	require.Contains(t, ansi.Strip(m.View().Content), "◓ Updating sandbox connection", "action spinner did not advance")
	m.Update(operationMsg{target: "a", code: store.CodeManagement, notice: "Saved management choice"})
	view := ansi.Strip(m.View().Content)
	require.Contains(t, view, "Saved management choice")
	require.NotContains(t, view, "◓")
}
