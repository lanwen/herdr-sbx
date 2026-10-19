package panel

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// panelKeys supplies the actual available bindings to the standard Bubbles help
// renderer. Context changes the compact footer; the full page retains all keys.
type panelKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k panelKeys) ShortHelp() []key.Binding  { return k.short }
func (k panelKeys) FullHelp() [][]key.Binding { return k.full }
func binding(keys, description string) key.Binding {
	return key.NewBinding(key.WithKeys(strings.Split(keys, "/")...), key.WithHelp(keys, description))
}
func (m *model) helpKeys() panelKeys {
	helpKey := binding("?", "help")
	closeKey := binding("q", "close")
	context := binding("enter", "details")
	arrows := binding("←/→", "cycle tabs")
	if m.page == "Plugins" {
		context = binding("space", "auto-sync")
	}
	if m.focus == "plugins" {
		context = binding("enter/space", "cycle choice")
	}
	if m.focus == "actions" || m.focus == "errors" || m.page == "Status" {
		context = binding("enter", "activate")
	}
	if m.focus == "events" {
		context = binding("enter/space", "expand/collapse")
	}
	if m.focus == "diagnostics" {
		context = binding("enter/space", "new/all")
		arrows = binding("←/→", "new/all")
	}
	if m.dialog != "" {
		context = binding("enter", "activate")
	}
	if m.input != nil {
		context = binding("enter", "save")
		if m.input.kind == "filter" {
			context = binding("enter", "accept")
		}
	}
	short := []key.Binding{helpKey, context, closeKey}
	if m.focus == "diagnostics" {
		short = []key.Binding{helpKey, arrows, context, closeKey}
	}
	if m.focus == "errors" {
		short = []key.Binding{helpKey, context, binding("d", "dismiss"), binding("n", "new/all"), closeKey}
	}
	if m.dialog != "" {
		short = []key.Binding{binding("esc", "cancel"), context, binding("tab", "focus")}
	}
	if m.input != nil {
		escape := "cancel"
		if m.input.kind == "filter" {
			escape = "back"
		}
		short = []key.Binding{binding("esc", escape), context, binding("ctrl+u", "clear")}
	}
	groups := [][]key.Binding{
		{binding("↑/↓", "rows/details/actions"), binding("home/end", "first/last"), binding("pgup/pgdown", "page"), binding("enter", "expand/details"), binding("tab/shift+tab", "next/previous tab"), arrows, binding("esc", "back/cancel")},
		{binding("w", "watch/unwatch"), binding("r", "rename herdr label"), binding("enter/space", "cycle herdr plugin sync"), binding("i", "inherit herdr plugin default"), binding("/", "fuzzy filter"), binding("ctrl+u", "clear input"), binding("enter", "save input")},
		{binding("s", "status"), binding("p", "herdr plugin defaults"), binding("b", "status / auto-sync"), binding("e", "diagnostics"), binding("d", "dismiss diagnostic"), binding("n", "new/all diagnostics"), binding("c", "refresh observations"), helpKey, closeKey, binding("ctrl+c", "cancel/close")},
	}
	return panelKeys{short, groups}
}
func (m *model) renderHelp(width int, full bool) string {
	component := help.New()
	component.SetWidth(max(1, width))
	component.ShowAll = full
	// The containing renderer owns colors, including NO_COLOR being present but
	// empty. Unstyled help keeps layout independent of Lip Gloss terminal probing.
	component.Styles = help.Styles{}
	keys := m.helpKeys()
	if full && width < 105 {
		var all []key.Binding
		for _, group := range keys.full {
			all = append(all, group...)
		}
		keys.full = [][]key.Binding{all}
	}
	rendered := ansi.Strip(component.View(keys))
	if !full {
		return truncate(rendered, max(1, width))
	}
	return rendered
}
func (m *model) helpLines() []line {
	var lines []line
	for _, text := range strings.Split(m.renderHelp(max(1, m.width-4), true), "\n") {
		lines = append(lines, textLine(ink(colorMuted, text)))
	}
	return lines
}
