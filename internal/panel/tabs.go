package panel

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *model) tabStrip() line {
	pages := []string{"Sandboxes", "Status", "Plugins"}
	active := 0
	for i, page := range pages {
		if page == m.page {
			active = i
		}
	}
	l := textLine(ink(colorSeparator, "╭─ "))
	count, padding := len(pages), "  "
	if m.width < 45 {
		padding = ""
	}
	if m.width < 32 {
		count = 1
	}
	for i := 0; i < count; i++ {
		page := pages[(active+i)%len(pages)]
		if i > 0 {
			l.text += " "
		}
		text := padding + page + padding
		x := ansi.StringWidth(l.text)
		if m.page == page {
			text = ink(colorAccent, text)
		} else {
			text = ink(colorMuted, text)
		}
		l.text += text
		l.hits = append(l.hits, hit{x: x, w: ansi.StringWidth(text), action: "page", id: page})
	}
	l.text += ink(colorSeparator, strings.Repeat("─", max(0, m.width-ansi.StringWidth(l.text)-1))+"╮")
	return l
}
