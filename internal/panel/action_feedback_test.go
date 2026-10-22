package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestDetailsConfirmationUsesHintWithLocalControls(t *testing.T) {
	m := modelFixture(t)
	m.expanded, m.selected, m.focus = "b", "b", "actions"
	m.dialog, m.dialogTarget, m.dialogReason = "fix-herdr", "b", "0.9.3"
	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	details := strings.Index(view, "beta (stopped)")
	confirm := strings.Index(view, "Fix herdr in beta?")
	if details < 0 || hint < details || confirm < hint {
		t.Fatalf("confirmation did not stay in Hint below details: %s", view)
	}
	m.key(keyMsg(tea.KeyRight, ""))
	if m.button != 1 || m.dialog != "fix-herdr" || m.page != "Sandboxes" {
		t.Fatal("confirmation lost local keyboard focus")
	}
	m.View()
	for _, h := range m.hits {
		if h.action == "dialog" && h.id == "0" {
			if h.y <= strings.Count(view[:hint], "\n") {
				t.Fatal("confirmation mouse target is outside Hint")
			}
			m.click(h.x, h.y)
			if m.dialog != "" || m.expanded != "b" {
				t.Fatal("mouse Cancel did not preserve details")
			}
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
	if hint < 0 || !strings.Contains(first[hint:], "◐ Updating sandbox connection for alpha") {
		t.Fatalf("active action missing from Hint: %s", first)
	}
	m.ticks++
	if !strings.Contains(ansi.Strip(m.View().Content), "◓ Updating sandbox connection") {
		t.Fatal("action spinner did not advance")
	}
	m.Update(operationMsg{target: "a", code: store.CodeManagement, notice: "Saved management choice"})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Saved management choice") || strings.Contains(view, "◓") {
		t.Fatalf("completion did not replace the spinner: %s", view)
	}
}
