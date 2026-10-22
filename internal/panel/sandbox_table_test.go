package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestSandboxColumnsPreserveNameBeforeLabel(t *testing.T) {
	for _, width := range []int{30, 60, 72, 110} {
		m := modelFixture(t)
		m.width = width
		m.rows[0].Sandbox.Name = "claude-api"
		m.rows[0].Label = strings.Repeat("label", 30)
		c := m.sandboxColumns(m.rows)
		if c.name < 10 {
			t.Fatalf("name clipped before label at %d: %+v", width, c)
		}
		if ansi.StringWidth(c.header()) != width-4 {
			t.Fatalf("invalid width: %+v", c)
		}
		row := c.render("    ", "✓", m.rows[0].Sandbox.Name, m.rows[0].Label, "Ready")
		if ansi.StringWidth(row) != width-4 {
			t.Fatalf("row width: %q", row)
		}
	}
}
func TestEnabledSymbolsAndPresence(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	r := m.rows[0]
	if enabledMark(r) != ink(colorAccent, "✓") {
		t.Fatal("Enabled must be blue checkmark")
	}
	r.Enabled = store.Disabled
	disabled := enabledMark(r)
	r.Enabled = store.Enabled
	r.EntryPresent = false
	if enabledMark(r) != disabled || ansi.Strip(disabled) != "·" {
		t.Fatal("missing and disabled must share muted dot")
	}
	r.EntryPresent = true
	r.Sandbox.Status = "stopped"
	native := false
	r.NativeEnabled = &native
	if ansi.Strip(enabledMark(r)) != "✓" {
		t.Fatal("native stopped hold replaced saved intent")
	}
}
func TestExpandedStatusAndInlineRename(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.Stale = true
	m.expanded = "a"
	rows := m.sandboxLines()
	if !strings.Contains(ansi.Strip(rows[3].text), "alpha (running") || !strings.Contains(ansi.Strip(rows[3].text), "API desk") {
		t.Fatalf("expanded identity missing from border: %q", rows[3].text)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "running (stale)") {
		t.Fatal("full stale runtime absent")
	}
	m.beginRename()
	m.input.text = strings.Repeat("label ", 30)
	m.input.cursor = len([]rune(m.input.text))
	if !strings.Contains(ansi.Strip(m.View().Content), "label █") {
		t.Fatal("rename cursor clipped")
	}
}
