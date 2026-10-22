package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestDiagnosticHeaderArrowsSelectNewAndAll(t *testing.T) {
	for _, page := range []string{"Sandboxes", "Status", "Plugins"} {
		t.Run(page, func(t *testing.T) {
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
				if m.showAllErrors != step.all || m.focus != "diagnostics" || m.page != page {
					t.Fatalf("key %v: all=%v focus=%s page=%s", step.key, m.showAllErrors, m.focus, m.page)
				}
				wantVisible := 1
				if step.all {
					wantVisible = 2
				}
				if len(m.visibleErrors()) != wantVisible {
					t.Fatalf("visible groups=%d want %d", len(m.visibleErrors()), wantVisible)
				}
				title := m.diagnosticTitle()
				if !strings.Contains(ansi.Strip(title), "Errors and warnings · New (2) | All (5)") || strings.Contains(title, "3 active hidden") == step.all {
					t.Fatalf("incorrect diagnostic title: %q", title)
				}
			}
			for _, full := range []bool{false, true} {
				if help := strings.Join(strings.Fields(ansi.Strip(m.renderHelp(110, full))), " "); !strings.Contains(help, "←/→ new/all") || strings.Contains(help, "←/→ cycle tabs") {
					t.Fatalf("incorrect header help: %q", help)
				}
			}
		})
	}
}

func TestDiagnosticHeaderRetainsTabCyclingAndActivation(t *testing.T) {
	m := modelFixture(t)
	m.focus = "diagnostics"
	m.key(keyMsg(tea.KeyTab, ""))
	if m.page != "Status" || m.focus != "table" {
		t.Fatal("Tab from diagnostic header did not cycle tabs")
	}
	m.focus = "diagnostics"
	m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.page != "Sandboxes" || m.focus != "table" {
		t.Fatal("Shift+Tab from diagnostic header did not reverse tabs")
	}
	m.focus = "diagnostics"
	m.key(keyMsg(tea.KeyEnter, ""))
	if !m.showAllErrors {
		t.Fatal("Enter did not toggle diagnostic view")
	}
	m.key(keyMsg(tea.KeySpace, " "))
	if m.showAllErrors {
		t.Fatal("Space did not toggle diagnostic view")
	}
	m.hitAction(hit{action: "diagnostic-toggle"})
	if !m.showAllErrors {
		t.Fatal("mouse header did not toggle diagnostic view")
	}
	m.focus = "errors"
	m.key(keyMsg(tea.KeyRight, ""))
	if m.page != "Sandboxes" || m.focus != "errors" {
		t.Fatal("Right from a diagnostic entry switched tabs or left its selection")
	}
}
