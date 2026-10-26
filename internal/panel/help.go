package panel

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/registration"
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
	localKeys := m.localHelpBindings(back)
	context, toggle, rename, inherit, arrows, back := localKeys.context, localKeys.toggle, localKeys.rename, localKeys.inherit, localKeys.arrows, localKeys.back
	dismiss, diagnostics, newAll, refresh, filter := m.globalHelpBindings()
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
	short := m.shortHelpBindings(helpKey, closeKey, localKeys, dismiss, newAll)
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

type localHelpBindings struct{ context, toggle, rename, inherit, arrows, back key.Binding }

func (m *model) localHelpBindings(back key.Binding) localHelpBindings {
	keys := localHelpBindings{back: back}
	m.sandboxHelpBindings(&keys)
	m.pluginDefaultHelpBindings(&keys)
	m.pluginOverrideHelpBindings(&keys)
	m.diagnosticHelpBindings(&keys)
	m.managementHelpBindings(&keys)
	return keys
}
func (m *model) sandboxHelpBindings(keys *localHelpBindings) {
	if m.page != "Sandboxes" {
		return
	}
	row, ok := m.row()
	if !ok {
		return
	}
	toggleAvailable := row.AssociationIssue == "" && !m.pending[row.Sandbox.ID] && (row.EntryPresent || !strings.EqualFold(row.Sandbox.Status, "stopped"))
	primary := "enable"
	if row.EntryPresent && row.Enabled == store.Enabled {
		primary = "disable"
	}
	if (m.focus == "table" || m.focus == "actions") && toggleAvailable {
		keys.toggle = binding("space", primary)
	}
	switch m.focus {
	case "table":
		m.sandboxTableHelp(keys, row)
	case "actions":
		m.sandboxActionHelp(keys, row, toggleAvailable)
	}
}
func (m *model) sandboxTableHelp(keys *localHelpBindings, row registration.SandboxRow) {
	if len(m.sandboxActions(row)) > 0 || len(m.plugins) > 0 {
		keys.context = binding("enter/→", "details")
	}
	if row.EntryPresent && row.AssociationIssue == "" && !m.pending[row.Sandbox.ID] {
		keys.rename = binding("r", "rename herdr label")
	}
}

func (m *model) pluginDefaultHelpBindings(keys *localHelpBindings) {
	if m.page != "Plugins" || m.focus != "table" {
		return
	}
	items := m.visiblePlugins()
	if m.pluginIndex < 0 || m.pluginIndex >= len(items) {
		return
	}
	p := items[m.pluginIndex]
	if m.pending["plugin:"+p.ID] || (!p.Enabled && p.UnsupportedReason != "") {
		return
	}
	description := "enable herdr plugin sync"
	if p.Enabled {
		description = "disable herdr plugin sync"
	}
	keys.context = binding("enter/space", description)
}

func (m *model) pluginOverrideHelpBindings(keys *localHelpBindings) {
	if m.page != "Sandboxes" || m.focus != "plugins" {
		return
	}
	if !m.helpOpen {
		keys.back = binding("esc/←", "list")
	}
	r, ok := findRow(m.rows, m.expanded)
	if !ok || m.pending[r.Sandbox.ID] || m.detailIndex < 0 || m.detailIndex >= len(m.plugins) {
		return
	}
	m.overrideChoiceHelp(keys, r, m.plugins[m.detailIndex])
}
func (m *model) overrideChoiceHelp(keys *localHelpBindings, r registration.SandboxRow, p guestplugins.Choice) {
	value, overridden := m.settings.Overrides[store.StatusKey(r.Scope, r.Sandbox.ID)][p.ID]
	nextEnabled := !overridden || !value && m.settings.Plugins[p.ID].Enabled
	if p.UnsupportedReason == "" || !nextEnabled {
		keys.context = binding("enter/space", "cycle choice")
	}
	if overridden && (p.UnsupportedReason == "" || !m.settings.Plugins[p.ID].Enabled) {
		keys.inherit = binding("i", "inherit default")
	}
}

func (m *model) diagnosticHelpBindings(keys *localHelpBindings) {
	if m.focus == "events" && len(m.events) > 0 {
		keys.context = binding("enter/space", "expand/collapse")
	}
	if m.focus == "diagnostics" && len(m.errors) > 0 {
		keys.context = binding("enter/space", "new/all")
		keys.arrows = binding("←/→", "new/all")
	}
	if m.focus == "errors" && len(m.visibleErrors()) > 0 {
		keys.context = binding("enter", "recovery")
	}

}

func (m *model) managementHelpBindings(keys *localHelpBindings) {
	if m.page != "Status" || m.focus != "table" {
		return
	}
	actions := m.statusActions()
	if m.button < 0 || m.button >= len(actions) {
		return
	}
	a := actions[m.button]
	if a.id == "toggle" {
		keys.context = binding("enter/space", m.managementChoiceHelp())
		return
	}
	if a.id == "refresh" && m.canRefresh() || a.id == "ssh" {
		keys.context = binding("enter", strings.ToLower(a.label))
	}
}
func (m *model) managementChoiceHelp() string {
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
	return description
}

func (m *model) shortHelpBindings(helpKey, closeKey key.Binding, keys localHelpBindings, dismiss, newAll key.Binding) []key.Binding {
	context, toggle, back, arrows := keys.context, keys.toggle, keys.back, keys.arrows
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
	return short
}

func (m *model) globalHelpBindings() (dismiss, diagnostics, newAll, refresh, filter key.Binding) {
	if len(m.errors) > 0 {
		diagnostics = binding("e", "diagnostics")
	}
	if len(m.errors) > 0 && (m.focus == "errors" || m.focus == "diagnostics") {
		newAll = binding("n", "new/all")
	}
	if m.canDismissSelectedError() {
		dismiss = binding("d", "dismiss")
	}
	if m.canRefresh() {
		refresh = binding("c", "refresh")
	}
	if m.page == "Sandboxes" || m.page == "Plugins" {
		filter = binding("/", "fuzzy filter")
	}

	return
}

func (m *model) sandboxActionHelp(keys *localHelpBindings, row registration.SandboxRow, toggleAvailable bool) {
	actions := m.sandboxActions(row)
	if m.button >= 0 && m.button < len(actions) && !m.pending[row.Sandbox.ID] {
		a := actions[m.button]
		if a.id != "enable" && a.id != "disable" || toggleAvailable {
			keys.context = binding("enter", strings.ToLower(a.label))
		}
	}
	m.actionNavigationHelp(keys, len(actions))
}

func (m *model) canDismissSelectedError() bool {
	if m.focus != "errors" {
		return false
	}
	entries := m.visibleErrors()
	return m.errorIndex >= 0 && m.errorIndex < len(entries) && !entries[m.errorIndex].dismissed && !entries[m.errorIndex].firstSeen.IsZero()
}

func (m *model) actionNavigationHelp(keys *localHelpBindings, count int) {
	if m.button == 0 {
		if count > 1 {
			keys.arrows = binding("→", "next action")
		}
		if !m.helpOpen {
			keys.back = binding("esc/←", "list")
		}
	} else {
		keys.arrows = binding("←/→", "actions")
		if m.button == count-1 {
			keys.arrows = binding("←", "previous action")
		}
		if !m.helpOpen {
			keys.back = binding("esc", "list")
		}
	}
}
