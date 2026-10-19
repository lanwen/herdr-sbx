package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
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
		m.expanded = "a" // Moving into Errors must not collapse another row.
		m.selected = "b"
		m.key(keys.down)
		if m.focus != "diagnostics" {
			t.Fatal("Down skipped diagnostics header")
		}
		m.key(keys.down)
		if m.focus != "errors" || m.errorIndex != 0 || !m.reveal || m.expanded != "a" {
			t.Fatal("Down from the last row did not reveal the first error")
		}
		m.key(keys.down)
		if m.errorIndex != 1 {
			t.Fatal("Down did not select the next error")
		}
		m.key(keys.up)
		if m.focus != "errors" || m.errorIndex != 0 {
			t.Fatal("Up did not select the previous error")
		}
		m.key(keys.up)
		if m.focus != "diagnostics" {
			t.Fatal("Up skipped diagnostics header")
		}
		m.key(keys.up)
		if m.focus != "table" || m.selected != "b" || m.expanded != "a" || !m.reveal {
			t.Fatal("Up from the first error did not return to the last sandbox")
		}
	}
}

func TestExpandedLastSandboxTraversesControlsBeforeErrorsAndReturns(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.errors = navigationErrors()
	m.plugins = []guestplugins.Choice{{ID: "first.plugin"}, {ID: "second.plugin"}}
	m.selected, m.expanded = "b", "b"
	m.rows[1].Management, m.rows[1].Owned = store.Watched, true
	m.rows[1].Status.Connection = "ready"
	lastAction := len(m.sandboxActions(m.rows[1])) - 1
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "plugins" || m.detailIndex != 0 {
		t.Fatal("last expanded sandbox skipped its plugins")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "actions" || m.button != 0 {
		t.Fatal("last expanded sandbox skipped its actions")
	}
	for m.button < lastAction {
		m.key(keyMsg(tea.KeyDown, ""))
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "diagnostics" {
		t.Fatal("Down skipped diagnostics header")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "errors" || m.errorIndex != 0 || m.expanded != "b" {
		t.Fatal("last sandbox action did not lead into Errors")
	}
	if content := ansi.Strip(m.View().Content); !strings.Contains(content, "> H0002 · alpha") {
		t.Fatalf("first error not revealed below long details: %s", content)
	}
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	if m.focus != "actions" || m.button != lastAction || m.expanded != "b" || m.selected != "b" {
		t.Fatal("Up did not return to the expanded sandbox's final action")
	}
	m.View()
	found := false
	for _, h := range m.hits {
		if h.action == "action" && h.id == m.sandboxActions(m.rows[1])[lastAction].id {
			found = true
		}
	}
	if !found {
		t.Fatal("returning from Errors did not reveal the final action")
	}
}

func TestEmptyFilteredTableCanReachErrorsWithoutClearingFilter(t *testing.T) {
	m := modelFixture(t)
	m.queries["Sandboxes"] = "no match"
	m.errors = navigationErrors()
	m.ensureSelection()
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "diagnostics" {
		t.Fatal("Down skipped diagnostics header")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "errors" || m.errorIndex != 0 {
		t.Fatal("empty table could not reach Errors")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	if m.focus != "table" || m.queries["Sandboxes"] != "no match" || len(m.visibleRows()) != 0 || m.scroll != 0 {
		t.Fatal("returning to empty table changed its filter or visibility")
	}
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
			if m.focus != "table" || context != "filter" && m.selected != "b" {
				t.Fatal("boundary navigation captured a local control or entered absent Errors")
			}
			if context == "filter" && (m.input == nil || m.input.text != "draft" || m.input.cursor != 2) {
				t.Fatal("Down changed the active filter")
			}
			if context == "dialog" && (m.dialog != "startup" || m.button != 0) {
				t.Fatal("Down did not retain dialog-local navigation")
			}
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
		if m.focus != "errors" || m.errorIndex != 0 {
			t.Fatalf("%s did not lead into Errors", page)
		}
		m.key(keyMsg(tea.KeyUp, ""))
		m.key(keyMsg(tea.KeyUp, ""))
		if m.focus != "table" || page == "Status" && m.button != lastButton || page == "Plugins" && m.pluginSelected != "two" {
			t.Fatalf("%s did not return to its final control", page)
		}
	}
}

func TestErrorShortcutAndPagingRetainTheirBehavior(t *testing.T) {
	m := modelFixture(t)
	m.errors = navigationErrors()
	m.key(keyMsg('e', "e"))
	if m.focus != "errors" {
		t.Fatal("e no longer focuses Errors")
	}
	m.key(keyMsg(tea.KeyHome, ""))
	if m.focus != "errors" || m.errorIndex != 0 {
		t.Fatal("Home left Errors")
	}
	m.key(keyMsg(tea.KeyPgUp, ""))
	if m.focus != "errors" {
		t.Fatal("Page Up left Errors")
	}
	m.errorIndex, m.scroll = len(m.errors)-1, 3
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "errors" || m.errorIndex != len(m.errors)-1 || m.scroll != 4 {
		t.Fatal("Down after the last error changed existing scroll behavior")
	}
}

func TestResolvedLastErrorReturnsFocusToTable(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.errors = navigationErrors()
	m.focusErrors()
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}})
	if len(m.errors) != 0 || m.focus != "table" || !m.reveal || m.scroll != 0 {
		t.Fatal("clearing the last active error left focus in the vanished Errors section")
	}
}
