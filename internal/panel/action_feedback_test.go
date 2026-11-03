package panel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestStoppedEnableInvocationHasVisibleFeedback(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{keyMsg(tea.KeyEnter, ""), keyMsg(tea.KeySpace, " ")} {
		t.Run(key.String(), func(t *testing.T) {
			m := modelFixture(t)
			m.rows[0].Sandbox.Name, m.rows[0].Sandbox.Status = "claude-kubernetes-mcp-server", "stopped"
			m.rows[0].EntryPresent, m.rows[0].CreationRequested = false, false
			m.rows[0].Enabled, m.rows[0].Label = store.Disabled, ""
			m.expanded, m.focus, m.button = "a", "actions", 0
			command := m.key(key)
			frame := ansi.Strip(m.View().Content)
			pending := command != nil && m.pending["a"] && strings.Contains(frame, "Enabling")
			blocked := m.notice != "" && strings.Contains(frame, m.notice)
			require.True(t, pending || blocked, "Enable must visibly start or explain why it cannot act; full frame:\n%s", frame)
		})
	}
}

func TestPopupKeepsFailedActionObservationsVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {110, 32}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = size[0], size[1]
			m.expanded, m.focus, m.button = "a", "actions", 0
			m.rows[0].EntryPresent, m.rows[0].Label = false, ""
			m.rows[0].Enabled = store.Disabled
			for i := range 20 {
				row := m.rows[0]
				row.Sandbox.ID, row.Sandbox.Name = fmt.Sprintf("row-%d", i), fmt.Sprintf("codex-long-project-%d", i)
				m.rows = append(m.rows, row)
			}
			m.events = []store.EventRecord{{Code: store.CodePreparation, Scope: "a", Name: "alpha", Detail: "Sandbox setup was checked", At: time.Now()}}
			m.setEnabledFn = func(context.Context, string, string, store.EnabledChoice) error {
				return errors.New("guest SSH proxy is unavailable")
			}
			m.refreshing = true
			command := m.key(keyMsg(tea.KeyEnter, ""))
			require.NotNil(t, command)
			m.Update(command())
			frame := ansi.Strip(m.View().Content)
			require.Contains(t, frame, "Events", "the long sandbox list must leave observations visible; full frame:\n%s", frame)
			require.Contains(t, frame, store.CodeManagement, "the async failure must be visible without scrolling past every sandbox; full frame:\n%s", frame)
		})
	}
}

func TestActionFailureRemainsVisibleAfterBackgroundRefresh(t *testing.T) {
	m := modelFixture(t)
	m.setEnabledFn = func(context.Context, string, string, store.EnabledChoice) error {
		return errors.New("sandbox SSH proxy unavailable")
	}
	m.rows[0].EntryPresent = false
	m.expanded, m.focus = "a", "actions"
	m.refreshing = true
	command := m.key(keyMsg(tea.KeyEnter, ""))
	require.NotNil(t, command)
	m.Update(command())
	m.Update(observationMsg{generation: m.generation, rows: m.rows})
	require.Contains(t, ansi.Strip(m.View().Content), "sandbox SSH proxy unavailable")
	require.False(t, m.pending["a"])
}

func TestSeparateObservationViewportReachesHistoryAndPreservesSandbox(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 40, 12
	m.events = []store.EventRecord{
		{Code: store.CodePreparation, Scope: "a", Detail: "oldest event payload", Output: strings.Repeat("evidence line\n", 30) + "END_OF_EVIDENCE", At: time.Now().Add(-time.Minute)},
		{Code: store.CodePreparation, Scope: "a", Detail: "newest event payload", At: time.Now()},
	}
	m.errors = []uiError{{code: store.CodeManagement, scope: "a", detail: "current action failed"}}
	m.key(keyMsg(tea.KeyEnd, ""))
	m.View()
	_, visible := matchingPanelHit(m.hits, func(h hit) bool { return h.action == "select" && h.id == m.selected })
	require.True(t, visible, "selected bottom sandbox must stay visible")
	selected := m.selected
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "events", m.focus)
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEnd, ""))
	var seen strings.Builder
	for range 50 {
		frame := ansi.Strip(m.View().Content)
		require.Len(t, strings.Split(frame, "\n"), m.height)
		seen.WriteString(frame)
		m.key(keyMsg(tea.KeyPgDown, ""))
	}
	require.Contains(t, seen.String(), "oldest event payload")
	require.Contains(t, seen.String(), "END_OF_EVIDENCE")
	require.Equal(t, selected, m.selected, "reading evidence must not move the sandbox")
}

func TestCompactConfirmationKeepsConsentVisibleWhileExplanationScrolls(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 40, 12
	m.rows[0].Sandbox.Name = "claude-kubernetes-mcp-server"
	m.focus, m.dialog, m.dialogTarget, m.dialogReason, m.button = "actions", "fix-herdr", "a", "0.9.5", 1
	m.reveal = true
	var seen strings.Builder
	for range 20 {
		frame := ansi.Strip(m.View().Content)
		require.Len(t, strings.Split(frame, "\n"), m.height)
		require.Contains(t, frame, "claude-kubernetes-mcp-server", "the full consent target must remain visible")
		require.Contains(t, frame, "0.9.5")
		require.Contains(t, frame, "keeps panes")
		require.Contains(t, frame, "no forced restart")
		require.Contains(t, frame, "Cancel")
		require.Contains(t, frame, "Fix herdr")
		seen.WriteString(frame)
		m.key(keyMsg(tea.KeyPgDown, ""))
	}
	require.Contains(t, seen.String(), "installed executable")
	require.Contains(t, seen.String(), "failed handoff")
	for _, id := range []string{"0", "1"} {
		_, visible := matchingPanelHit(m.hits, func(h hit) bool { return h.action == "dialog" && h.id == id })
		require.True(t, visible, "consent buttons must keep their mouse targets")
	}
}
