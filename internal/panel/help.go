package panel

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// panelKeys supplies contextually available bindings to Bubbles in both views.
type panelKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k panelKeys) ShortHelp() []key.Binding  { return k.short }
func (k panelKeys) FullHelp() [][]key.Binding { return k.full }
func binding(keys, description string) key.Binding {
	actual := strings.Split(keys, "/")
	if keys == "/" {
		actual = []string{"/"}
	}
	for i, k := range actual {
		actual[i] = strings.NewReplacer("←", "left", "→", "right", "↑", "up", "↓", "down").Replace(k)
	}
	return key.NewBinding(key.WithKeys(actual...), key.WithHelp(keys, description))
}
func (m *model) canRefresh() bool {
	if m.refreshing {
		return false
	}
	for _, a := range m.statusActions() {
		if a.id == "refresh" {
			return true
		}
	}
	return false
}
func (m *model) helpKeys() panelKeys {
	if m.input != nil {
		description, escape := "save", "cancel"
		if m.input.kind == "filter" {
			description, escape = "accept", "back"
		}
		short := []key.Binding{binding("esc", escape), binding("enter", description), binding("ctrl+u", "clear")}
		return panelKeys{short, [][]key.Binding{short, {binding("←/→", "cursor"), binding("home/end", "start/end"), binding("backspace/delete", "delete"), binding("ctrl+c", "cancel")}}}
	}
	if m.dialog != "" {
		short := []key.Binding{binding("esc", "cancel"), binding("enter", "activate"), binding("tab", "focus")}
		return panelKeys{short, [][]key.Binding{short, {binding("←/→", "focus"), binding("shift+tab", "previous choice"), binding("ctrl+c", "cancel")}}}
	}
	helpKey, closeKey := binding("?", "help"), binding("q", "close")
	if m.popupQuitKey("esc") {
		closeKey = binding("esc/q", "close")
	}
	back := binding("esc", "back")
	if m.helpOpen {
		helpKey = binding("?", "close help")
		back = binding("esc", "close help")
	}
	var context, toggle, rename, inherit, arrows key.Binding
	row, hasRow := m.row()
	toggleAvailable := hasRow && row.AssociationIssue == "" && !m.pending[row.Sandbox.ID] && (row.EntryPresent || !strings.EqualFold(row.Sandbox.Status, "stopped"))
	primary := "enable"
	if hasRow && row.EntryPresent && row.Enabled == store.Enabled {
		primary = "disable"
	}
	if m.page == "Sandboxes" && (m.focus == "table" || m.focus == "actions") && toggleAvailable {
		toggle = binding("space", primary)
	}
	if m.page == "Sandboxes" && m.focus == "table" && hasRow {
		if len(m.sandboxActions(row)) > 0 || len(m.plugins) > 0 {
			context = binding("enter/→", "details")
		}
		if row.EntryPresent && row.AssociationIssue == "" && !m.pending[row.Sandbox.ID] {
			rename = binding("r", "rename herdr label")
		}
	}
	if m.page == "Sandboxes" && m.focus == "actions" && hasRow {
		actions := m.sandboxActions(row)
		if m.button >= 0 && m.button < len(actions) && !m.pending[row.Sandbox.ID] {
			a := actions[m.button]
			if a.id != "enable" && a.id != "disable" || toggleAvailable {
				context = binding("enter", strings.ToLower(a.label))
			}
		}
		if m.button == 0 {
			if len(actions) > 1 {
				arrows = binding("→", "next action")
			}
			if !m.helpOpen {
				back = binding("esc/←", "list")
			}
		} else {
			arrows = binding("←/→", "actions")
			if m.button == len(actions)-1 {
				arrows = binding("←", "previous action")
			}
			if !m.helpOpen {
				back = binding("esc", "list")
			}
		}
	}
	if m.page == "Plugins" && m.focus == "table" {
		items := m.visiblePlugins()
		if m.pluginIndex >= 0 && m.pluginIndex < len(items) {
			p := items[m.pluginIndex]
			if !m.pending["plugin:"+p.ID] && (p.Enabled || p.UnsupportedReason == "") {
				description := "enable herdr plugin sync"
				if p.Enabled {
					description = "disable herdr plugin sync"
				}
				context = binding("enter/space", description)
			}
		}
	}
	if m.page == "Sandboxes" && m.focus == "plugins" {
		if !m.helpOpen {
			back = binding("esc/←", "list")
		}
		if r, ok := findRow(m.rows, m.expanded); ok && !m.pending[r.Sandbox.ID] && m.detailIndex >= 0 && m.detailIndex < len(m.plugins) {
			p := m.plugins[m.detailIndex]
			value, overridden := m.settings.Overrides[store.StatusKey(r.Scope, r.Sandbox.ID)][p.ID]
			nextEnabled := !overridden || !value && m.settings.Plugins[p.ID].Enabled
			if p.UnsupportedReason == "" || !nextEnabled {
				context = binding("enter/space", "cycle choice")
			}
			if overridden && (p.UnsupportedReason == "" || !m.settings.Plugins[p.ID].Enabled) {
				inherit = binding("i", "inherit default")
			}
		}
	}
	if m.focus == "events" && len(m.events) > 0 {
		context = binding("enter/space", "expand/collapse")
	}
	if m.focus == "diagnostics" && len(m.errors) > 0 {
		context = binding("enter/space", "new/all")
		arrows = binding("←/→", "new/all")
	}
	if m.focus == "errors" && len(m.visibleErrors()) > 0 {
		context = binding("enter", "recovery")
	}
	if m.page == "Status" && m.focus == "table" {
		actions := m.statusActions()
		if m.button >= 0 && m.button < len(actions) {
			a := actions[m.button]
			if a.id == "toggle" {
				description := "enable Management"
				if m.workerEnabled {
					description = "disable Management"
				}
				if m.workerControlError {
					description = "retry disable Management"
					if m.workerEnabled {
						description = "retry enable Management"
					}
				}
				context = binding("enter/space", description)
			} else if a.id == "refresh" && m.canRefresh() || a.id == "ssh" {
				context = binding("enter", strings.ToLower(a.label))
			}
		}
	}
	var dismiss, diagnostics, newAll, refresh, filter key.Binding
	if len(m.errors) > 0 {
		diagnostics = binding("e", "diagnostics")
	}
	if len(m.errors) > 0 && (m.focus == "errors" || m.focus == "diagnostics") {
		newAll = binding("n", "new/all")
	}
	if m.focus == "errors" {
		entries := m.visibleErrors()
		if m.errorIndex >= 0 && m.errorIndex < len(entries) && !entries[m.errorIndex].dismissed && !entries[m.errorIndex].firstSeen.IsZero() {
			dismiss = binding("d", "dismiss")
		}
	}
	if m.canRefresh() {
		refresh = binding("c", "refresh")
	}
	if m.page == "Sandboxes" || m.page == "Plugins" {
		filter = binding("/", "fuzzy filter")
	}
	navigation := []key.Binding{binding("↑/↓", "move"), binding("home/end", "first/last"), binding("pgup/pgdown", "page"), arrows, back, binding("tab/shift+tab", "next/previous tab")}
	if m.focus == "diagnostics" {
		navigation[1] = key.Binding{}
	}
	if m.helpOpen {
		navigation[0] = binding("↑/↓", "scroll help")
		navigation[1] = binding("home/end", "first/last help")
		navigation[2] = binding("pgup/pgdown", "page help")
	}
	local := []key.Binding{context, toggle, rename, inherit, filter}
	global := []key.Binding{binding("s", "status"), binding("p", "herdr plugin defaults"), binding("b", "status / Management"), diagnostics, dismiss, newAll, refresh, helpKey, closeKey, binding("ctrl+c", "cancel/close")}
	short := []key.Binding{helpKey, context, toggle, closeKey}
	if m.width < 60 {
		short = []key.Binding{helpKey, toggle, context, closeKey}
	}
	if m.focus == "actions" || m.focus == "plugins" {
		short = append(short, back)
	}
	if m.focus == "errors" {
		short = []key.Binding{helpKey, context, dismiss, newAll, closeKey}
	}
	if m.focus == "diagnostics" {
		short = []key.Binding{helpKey, arrows, context, closeKey}
	}
	if m.width >= 100 {
		short = append(short, binding("tab/shift+tab", "tabs"))
	}
	if m.helpOpen {
		short = []key.Binding{helpKey, back, binding("↑/↓", "scroll help")}
	}
	return panelKeys{short, [][]key.Binding{navigation, local, global}}
}
func (m *model) renderHelp(width int, full bool) string {
	component := help.New()
	component.SetWidth(max(1, width))
	component.ShowAll = full
	component.Styles = help.Styles{}
	if colorsEnabled() {
		keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0C0"))
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
		separator := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("#373D56"))
		component.Styles = help.Styles{ShortKey: keyStyle, FullKey: keyStyle, ShortDesc: descStyle, FullDesc: descStyle, ShortSeparator: separator, FullSeparator: separator, Ellipsis: separator}
	}
	keys := m.helpKeys()
	if full && width < 105 {
		var all []key.Binding
		for _, group := range keys.full {
			all = append(all, group...)
		}
		keys.full = [][]key.Binding{all}
		component.SetWidth(0)
	}
	rendered := component.View(keys)
	if !colorsEnabled() {
		rendered = ansi.Strip(rendered)
	} else {
		// Preserve the containing terminal surface when Lip Gloss resets its styles.
		rendered = strings.NewReplacer("\x1b[m", "\x1b[22m\x1b[39m", "\x1b[0m", "\x1b[22m\x1b[39m").Replace(rendered)
	}
	if !full {
		return truncate(rendered, max(1, width))
	}
	return ansi.Wrap(rendered, max(1, width), "")
}
func (m *model) helpLines() []line {
	var lines []line
	for _, text := range strings.Split(m.renderHelp(max(1, m.width-4), true), "\n") {
		lines = append(lines, textLine(text))
	}
	return lines
}
func (m *model) helpKey(keyPress tea.KeyPressMsg) tea.Cmd {
	switch keyPress.String() {
	case "?", "esc":
		m.helpOpen = false
		return nil
	case "up", "k":
		m.helpScroll = max(0, m.helpScroll-1)
		return nil
	case "down", "j":
		m.helpScroll++
		return nil
	case "pgup":
		m.helpScroll = max(0, m.helpScroll-max(1, m.height-4))
		return nil
	case "pgdown":
		m.helpScroll += max(1, m.height-4)
		return nil
	case "home":
		m.helpScroll = 0
		return nil
	case "end":
		m.helpScroll = len(m.helpLines())
		return nil
	}
	for _, group := range m.helpKeys().full {
		for _, b := range group {
			if key.Matches(keyPress, b) {
				m.helpOpen = false
				return m.key(keyPress)
			}
		}
	}
	return nil
}
func (m *model) helpView() tea.View {
	m.hits = nil
	lines := outline(m.helpLines(), "Help · "+m.page, m.width, 0)
	footer := []line{textLine(m.renderHelp(m.width, false))}
	if m.managementCancelling() && m.width < 42 {
		footer = append(footer, textLine(ink(colorMuted, "Cancelling")))
	}
	footer = append(footer, m.statusFooter())
	capacity := max(1, m.height-len(footer)-1)
	body := lines[1:]
	m.helpScroll = max(0, min(m.helpScroll, max(0, len(body)-capacity)))
	frame := []line{lines[0]}
	frame = append(frame, body[m.helpScroll:min(len(body), m.helpScroll+capacity)]...)
	for len(frame) < m.height-len(footer) {
		frame = append(frame, textLine(""))
	}
	frame = append(frame, footer...)
	var text strings.Builder
	for i, l := range frame {
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(m.paintLine(l))
	}
	m.renderedFrame = text.String()
	view := tea.NewView(m.renderedFrame)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}
