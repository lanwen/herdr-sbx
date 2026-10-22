package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSandboxColumnsPreserveNameBeforeLabel(t *testing.T) {
	for _, width := range []int{30, 60, 72, 110} {
		m := modelFixture(t)
		m.width = width
		m.rows[0].Sandbox.Name = "claude-api"
		m.rows[0].Label = strings.Repeat("label", 30)
		c := m.sandboxColumns(m.rows)
		require.False(t, c.name < 10, "name clipped before label at %d: %+v", width, c)
		require.Equal(t, width-4, ansi.StringWidth(c.header()), "invalid width: %+v", c)
		row := c.render("    ", "✓", m.rows[0].Sandbox.Name, m.rows[0].Label, "Ready")
		require.Equal(t, width-4, ansi.StringWidth(row), "row width: %q", row)
	}
}
func TestEnabledSymbolsAndPresence(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	r := m.rows[0]
	require.Equal(t, ink(colorAccent, "✓"), enabledMark(r), "Enabled must be blue checkmark")
	r.Enabled = store.Disabled
	disabled := enabledMark(r)
	r.Enabled = store.Enabled
	r.EntryPresent = false
	require.False(t, enabledMark(r) != disabled)
	require.EqualValues(t, "·", ansi.Strip(disabled))
	r.EntryPresent = true
	r.Sandbox.Status = "stopped"
	native := false
	r.NativeEnabled = &native
	require.Equal(t, "✓", ansi.Strip(enabledMark(r)), "native stopped hold replaced saved intent")
}
func TestExpandedStatusAndInlineRename(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.Stale = true
	m.expanded = "a"
	rows := m.sandboxLines()
	require.Contains(t, ansi.Strip(rows[3].text), "alpha (running")
	require.Contains(t, ansi.Strip(rows[3].text), "API desk")
	require.Contains(t, ansi.Strip(m.View().Content), "running (stale)", "full stale runtime absent")
	m.beginRename()
	m.input.text = strings.Repeat("label ", 30)
	m.input.cursor = len([]rune(m.input.text))
	require.Contains(t, ansi.Strip(m.View().Content), "label █", "rename cursor clipped")
}
