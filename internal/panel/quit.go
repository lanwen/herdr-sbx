package panel

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const quitHint = "Press esc, q or ctrl+c again to quit"

type quitTimeoutMsg time.Time

func (m *model) quitHintLines(now time.Time) []line {
	seconds := max(1, int((m.quitUntil.Sub(now)+time.Second-1)/time.Second))
	suffix := fmt.Sprintf("...%ds", seconds)
	var lines []line
	for _, part := range wrapHint(quitHint+" "+suffix, max(1, m.width-4)) {
		main, countdown := strings.CutSuffix(part, suffix)
		text := ink(colorInfo, main)
		if countdown {
			text += ink(colorMuted, suffix)
		}
		lines = append(lines, textLine(text))
	}
	return lines
}

// Popup pane commands have entrypoint context but no tiled pane ID.
func popupManager() bool {
	return os.Getenv("HERDR_ENV") == "1" &&
		os.Getenv("HERDR_PLUGIN_ID") == "lanwen.herdr-sbx" &&
		os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "status" &&
		os.Getenv("HERDR_PANE_ID") == ""
}

func (m *model) popupQuitKey(k string) bool {
	if !m.popup || m.input != nil || m.dialog != "" {
		return false
	}
	if k == "q" || k == "ctrl+c" {
		return true
	}
	return k == "esc" && !m.helpOpen &&
		!(m.textSelection != nil && m.textSelection.pressed) &&
		m.page == "Sandboxes" && m.focus == "table" && m.queries[m.page] == ""
}

func (m *model) confirmQuit() tea.Cmd {
	now := time.Now()
	if now.Before(m.quitUntil) {
		m.quitUntil = time.Time{}
		return m.quit()
	}
	m.quitUntil = now.Add(5 * time.Second)
	deadline := m.quitUntil
	return tea.Tick(time.Until(deadline), func(time.Time) tea.Msg { return quitTimeoutMsg(deadline) })
}

func (m *model) quit() tea.Cmd {
	if len(m.pending) > 0 {
		m.closing = true
		m.notice = "Finishing active work and cleanup before closing…"
		return nil
	}
	return tea.Quit
}
