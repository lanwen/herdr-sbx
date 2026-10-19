package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestDetailsConfirmationUsesHintWithLocalControls(t *testing.T) {
	m := modelFixture(t)
	m.expanded, m.selected, m.focus = "b", "b", "actions"
	m.dialog, m.dialogTarget, m.dialogReason = "adopt", "b", "… Checking eligibility"
	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	details := strings.Index(view, "╭─ Details")
	confirm := strings.Index(view, "Adopt beta?")
	if details < 0 || hint < details || confirm < hint || !strings.Contains(view, "◐ Checking eligibility") {
		t.Fatalf("confirmation did not stay in Hint below details: %s", view)
	}
	m.key(keyMsg(tea.KeyRight, ""))
	if m.button != 1 || m.dialog != "adopt" || m.page != "Sandboxes" {
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
	if hint < 0 || !strings.Contains(first[hint:], "◐ Saving sandbox management choice for alpha") {
		t.Fatalf("active action missing from Hint: %s", first)
	}
	m.ticks++
	if !strings.Contains(ansi.Strip(m.View().Content), "◓ Saving sandbox management choice") {
		t.Fatal("action spinner did not advance")
	}
	m.Update(operationMsg{target: "a", code: store.CodeManagement, notice: "Saved management choice"})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Saved management choice") || strings.Contains(view, "◓") {
		t.Fatalf("completion did not replace the spinner: %s", view)
	}
}

func TestAdoptionConfirmationStartsWorkInHint(t *testing.T) {
	m := modelFixture(t)
	m.expanded, m.selected, m.focus = "b", "b", "actions"
	m.dialog, m.dialogTarget, m.button = "adopt", "b", 1
	m.adoption = &registration.Adoption{Sandbox: m.rows[1].Sandbox}
	cmd := m.key(keyMsg(tea.KeyEnter, ""))
	if cmd == nil || m.dialog != "" || !m.pending["b"] || m.expanded != "b" {
		t.Fatal("confirmation did not start adoption while keeping details open")
	}
	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	if hint < 0 || !strings.Contains(view[hint:], "◐ Adopting sandbox for beta") {
		t.Fatalf("adoption progress not in Hint: %s", view)
	}
}
