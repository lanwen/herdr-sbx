package panel

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func textLine(s string) line { return line{text: s} }
func controlLine(labels, actions, ids []string) line {
	l := line{}
	for i, label := range labels {
		if i > 0 {
			l.text += "  "
		}
		x := ansi.StringWidth(l.text)
		text := buttonStyle(label, strings.HasPrefix(label, "› "))
		l.text += text
		l.hits = append(l.hits, hit{x: x, w: ansi.StringWidth(text), action: actions[i], id: ids[i]})
	}
	return l
}
func (m *model) buttons(labels []string) line {
	actions := make([]string, len(labels))
	ids := make([]string, len(labels))
	for i := range labels {
		actions[i] = "dialog"
		ids[i] = fmt.Sprint(i)
		if i == m.button%len(labels) {
			labels[i] = "› " + labels[i]
		}
	}
	return controlLine(labels, actions, ids)
}
func symbol(level precheck.Level) string {
	switch level {
	case precheck.Ready:
		return "✓ Ready"
	case precheck.Blocked:
		return "✗ Failed"
	default:
		return "? Unknown"
	}
}
func workerText(state string) string {
	switch state {
	case "running":
		return "✓ Running"
	case "starting":
		return "… Starting"
	case "reconnecting":
		return "… Reconnecting"
	case "stopped":
		return "— Stopped"
	default:
		return "? Unknown"
	}
}
func truncate(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
func padded(s string, w int) string {
	s = truncate(s, w)
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func (m *model) renderedInput(width int) string {
	if m.input == nil {
		return ""
	}
	r := []rune(m.input.text)
	pos := min(m.input.cursor, len(r))
	left := string(r[:pos])
	right := string(r[pos:])
	for ansi.StringWidth(left)+1 > width && len(left) > 0 {
		_, n := utf8.DecodeRuneInString(left)
		left = left[n:]
	}
	caret := "│"
	if m.input.kind == "rename" {
		caret = "█"
	}
	return truncate(left+caret+right, max(1, width))
}
func (m *model) View() tea.View {
	if m.textSelection != nil {
		v := tea.NewView(m.textSelection.view())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	if m.helpOpen {
		return m.helpView()
	}
	m.hits = nil
	m.hintInBody = false
	pageTitle := m.page
	if pageTitle == "Plugins" {
		pageTitle = "Plugin defaults"
	}
	inlineConfirmation := m.dialog == "fix-herdr"
	var body []line
	if m.dialog != "" && !inlineConfirmation {
		body = m.dialogLines()
	} else if m.width < 30 || m.height < 10 {
		body = []line{textLine("Resize to at least 30 columns × 10 rows."), textLine("b Management · e diagnostics · q close")}
	} else {
		switch m.page {
		case "Sandboxes":
			body = m.sandboxLines()
		case "Plugins":
			body = m.pluginLines()
		case "Status":
			body = m.statusLines()
		case "Help":
			body = m.helpLines()
		}
	}
	body = outline(body, func() string {
		if m.dialog != "" && !inlineConfirmation {
			return "Action"
		}
		return pageTitle
	}(), m.width, 0)
	header := body[0]
	if (m.dialog == "" || inlineConfirmation) && (m.page == "Sandboxes" || m.page == "Status" || m.page == "Plugins") {
		header = m.tabStrip()
	}
	lines := []line{header}
	for _, h := range header.hits {
		h.y = 0
		m.hits = append(m.hits, h)
	}
	body = body[1:]

	var notices []line
	hint, hintColor := m.notice, colorInfo
	if m.noticeTarget == "worker" && m.managementCancelling() {
		hintColor = colorMuted
	}
	if hint != "" && m.width < 60 {
		if identity, _ := m.contextHint(); strings.HasPrefix(identity, "herdr plugin: ") {
			hint = identity + ". " + hint
		}
	}
	if hint == "" {
		hint, hintColor = m.contextHint()
	}
	var hintLines []line
	if inlineConfirmation {
		hintLines = m.dialogLines()
	} else {
		if hint != "" {
			hint = strings.ReplaceAll(productText(hint), "herdr-sbx", "the plugin")
			if m.noticeTarget != "" && m.pending[m.noticeTarget] && !(m.noticeTarget == "worker" && m.managementCancelling()) {
				hint = m.actionSpinner() + " " + hint
			}
			for _, part := range wrapHint(safeText(hint), max(1, m.width-4)) {
				hintLines = append(hintLines, textLine(ink(hintColor, part)))
			}
		}
	}
	if len(hintLines) > 0 {

		notices = append(notices, outline(hintLines, "Hint", m.width, 0)...)
	}
	if m.dialog == "" {
		if len(m.events) > 0 {
			var events []line
			if m.eventsExpanded {
				events = m.eventLines()
			}
			titleColor := colorSeparator
			if m.focus == "events" && m.eventIndex < 0 {
				titleColor = colorText
			}
			section := outline(events, ink(titleColor, m.eventTitle()), m.width, 0)
			section[0].hits = []hit{{x: 0, w: m.width, action: "event-section"}}
			body = append(body, section...)
		}
		if len(m.errors) > 0 {
			title := m.diagnosticTitle()
			details := m.errorLines()
			if ansi.StringWidth(title) > m.width-6 {
				// Keep the New/All switch visible when the title cannot hold it.
				heading, selector, _ := strings.Cut(title, " · ")
				title = heading
				selector, hidden, _ := strings.Cut(selector, " · ")
				controls := []line{{text: selector, hits: []hit{{x: 0, w: ansi.StringWidth(selector), action: "diagnostic-toggle"}}}}
				if hidden != "" {
					controls = append(controls, textLine(ink(colorMuted, hidden)))
				}
				details = append(controls, details...)
			}
			section := outline(details, title, m.width, 0)
			section[0].hits = []hit{{x: 0, w: m.width, action: "diagnostic-toggle"}}
			body = append(body, section...)
		}
	}
	// Long hints and confirmations scroll with the body instead of consuming
	// the selected control viewport. Keep the complete explanation available.
	if len(notices) > m.height-7 {
		m.hintInBody = true
		body = append(body, notices...)
		notices = nil
	}
	footer := m.statusFooter()
	footerLines := []line{textLine(ink(colorMuted, m.renderHelp(m.width, false)))}
	if m.managementCancelling() && m.width < 42 {
		footerLines = append(footerLines, textLine(ink(colorMuted, "Cancelling")))
	}
	footerLines = append(footerLines, footer)
	fixed := len(notices) + len(footerLines)
	capacity := max(1, m.height-fixed-1)
	if m.reveal || m.focus == "errors" || m.focus == "diagnostics" || m.focus == "events" {
		target := -1
		for i, l := range body {
			for _, h := range l.hits {
				match := m.dialog != "" && h.action == "dialog" && h.id == fmt.Sprint(m.button%m.dialogButtonCount())
				if m.dialog == "" {
					switch m.focus {
					case "errors":
						match = h.action == "error" && h.id == fmt.Sprint(m.errorIndex)
					case "diagnostics":
						match = h.action == "diagnostic-toggle"
					case "events":
						match = m.eventIndex < 0 && h.action == "event-section" || m.eventIndex >= 0 && h.action == "event" && h.id == fmt.Sprint(m.eventIndex)
					case "table":
						match = (m.page == "Sandboxes" && h.action == "select" && h.id == m.selected) || (m.page == "Plugins" && h.action == "plugin" && h.id == m.pluginSelected) || (m.page == "Status" && ((m.button == 0 && h.action == "status-choice" && h.id == "on") || (h.action == "status-action" && h.id == fmt.Sprint(m.button))))
					case "plugins":
						match = h.action == "override" && len(m.plugins) > 0 && h.id == m.plugins[min(m.detailIndex, len(m.plugins)-1)].ID
					case "actions":
						if m.page == "Sandboxes" {
							for _, row := range m.rows {
								if row.Sandbox.ID == m.expanded {
									actions := m.sandboxActions(row)
									match = h.action == "action" && m.button >= 0 && m.button < len(actions) && h.id == actions[m.button].id
									break
								}
							}
						}
					}
				}
				// A diagnostic group has several clickable lines. Reveal its
				// beginning so long details do not scroll its reference away.
				if match && (m.focus != "errors" || target < 0) {
					target = i
				}
			}
		}
		if target >= 0 {
			if target < m.scroll && m.reveal {
				m.scroll = target
			}
			if (m.focus == "errors" || m.focus == "events" && m.eventIndex >= 0) && target+2 >= m.scroll+capacity {
				// Keep the selected cause and reference below its metadata,
				// rather than revealing only its first line at the viewport edge.
				m.scroll = max(0, target-min(2, max(0, capacity-3)))
			} else if target >= m.scroll+capacity {
				m.scroll = target - capacity + 1
			}
		}
		m.reveal = false
	}

	m.scroll = min(max(0, len(body)-capacity), m.scroll)
	for _, l := range body[m.scroll:min(len(body), m.scroll+capacity)] {
		y := len(lines)
		for _, h := range l.hits {
			h.y = y
			m.hits = append(m.hits, h)
		}
		lines = append(lines, l)
	}
	for len(lines) < m.height-fixed {
		lines = append(lines, textLine(""))
	}
	for _, l := range notices {
		y := len(lines)
		for _, h := range l.hits {
			h.y = y
			m.hits = append(m.hits, h)
		}
		lines = append(lines, l)
	}
	for _, footerLine := range footerLines {
		for _, h := range footerLine.hits {
			h.y = len(lines)
			m.hits = append(m.hits, h)
		}
		lines = append(lines, footerLine)
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		s := m.paintLine(l)
		b.WriteString(s)
	}
	m.renderedFrame = b.String()
	v := tea.NewView(m.renderedFrame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) contextHint() (string, string) {
	if m.page == "Status" && m.dialog == "" {
		if m.managementCancelling() {
			return "Cancelling", colorMuted
		}
		if m.pending["worker"] {
			return "Management change in progress.", colorInfo
		}
		if m.worker.Detail != "" {
			return safeText(m.worker.Detail), colorInfo
		}
		return "Management: " + ansi.Strip(m.managementStatus()) + ".", colorInfo
	}
	if m.dialog == "" && m.width < 60 {
		// Compact rows reserve room for choices; retain the complete focused ID.
		if m.page == "Plugins" && m.focus == "table" {
			plugins := m.visiblePlugins()
			if len(plugins) > 0 {
				return "herdr plugin: " + safeText(plugins[min(m.pluginIndex, len(plugins)-1)].ID), colorInfo
			}
		}
		if m.page == "Sandboxes" && m.focus == "plugins" && len(m.plugins) > 0 {
			return "herdr plugin: " + safeText(m.plugins[min(m.detailIndex, len(m.plugins)-1)].ID), colorInfo
		}
	}
	if m.dialog != "" || m.page != "Sandboxes" || (m.focus == "errors" || m.focus == "diagnostics" || m.focus == "events") {
		return "", ""
	}
	row, ok := findRow(m.rows, m.selected)
	if !ok {
		return "", ""
	}
	if m.focus == "actions" && m.expanded == m.selected {
		actions := m.sandboxActions(row)
		if m.button >= 0 && m.button < len(actions) {
			switch actions[m.button].id {
			case "enable":
				if !row.EntryPresent && strings.EqualFold(row.Sandbox.Status, "stopped") {
					return "Start the sandbox yourself before enabling its herdr connection.", colorMuted
				}
				return "Enable this sandbox's herdr connection. Herdr may start it when connecting.", colorAccent
			case "disable":
				return "Disable this sandbox's herdr connection and stop its plugin work. Keep its entry, label, and existing files.", colorInfo
			case "remove":
				return "Remove this disabled entry from herdr. Keep the sandbox and its files. Enable adds the entry again.", colorInfo
			case "rename":
				return "Rename changes this sandbox's label in herdr. Its sandbox name and connection stay the same.", colorInfo
			case "resync":
				return "Re-sync checks this sandbox and retries setup or selected herdr plugin sync. It checks for manually repaired plugins before building again.", colorInfo
			case "fix-herdr":
				return m.fixHerdrExplanation(row), colorInfo
			}
		}
	}
	if row.Status.Stale {
		detail := "Current observations are unavailable."
		if row.Status.Detail != "" {
			detail += " Last known status: " + safeText(row.Status.Detail)
		}
		if !row.Status.ObservedAt.IsZero() {
			detail += " Last observation " + row.Status.ObservedAt.Local().Format("2006-01-02 15:04:05") + "."
		}
		return detail, colorWarning
	}
	if row.AssociationIssue != "" {
		return row.AssociationIssue, colorWarning
	}
	if m.rowHasCode(row, store.CodeHerdrCompatibility) || strings.Contains(strings.ToLower(row.Status.Detail), "herdr in this sandbox") {
		return m.herdrCompatibilityHint(row), colorError
	}
	if row.Enabled == store.Enabled && row.EntryPresent && strings.EqualFold(row.Sandbox.Status, "stopped") && strings.EqualFold(row.Status.Connection, "waiting for start") {
		return "This sandbox is stopped. Its herdr entry is saved. Setup waits until it starts and Management is ON.", colorMuted
	}
	switch row.Status.Detail {
	case "already registered", "registered", "already registered and reachable", "registered and reachable":
		return "This sandbox is registered in herdr. Setup is complete.", colorSuccess
	}
	color := colorMuted
	switch strings.ToLower(row.Status.Connection) {
	case "ready", "connected", "registered":
		color = colorSuccess
	case "failed", "blocked", "error", "manual intervention required":
		color = colorError
	case "preparing", "installing", "pending":
		color = colorWarning
	}
	return strings.ReplaceAll(productText(row.Status.Detail), "herdr-sbx", "the plugin"), color
}

func wrapHint(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if ansi.StringWidth(lines[last])+1+ansi.StringWidth(word) > width {
			lines = append(lines, word)
		} else {
			lines[last] += " " + word
		}
	}
	return lines
}

func productText(s string) string {
	return strings.NewReplacer("Herdr-sbx", "herdr-sbx", "Herdr", "herdr").Replace(s)
}
func (m *model) filterLine(total, visible int, noun string) line {
	query := m.queries[m.page]
	if query == "" {
		query = "Filter…"
	}
	suffix := fmt.Sprintf("   %d of %d %s", visible, total, noun)
	if m.width < 60 {
		suffix = fmt.Sprintf("  %d/%d", visible, total)
	}
	actualQuery := m.queries[m.page]
	focused := m.input != nil && m.input.kind == "filter"
	if focused {
		actualQuery = m.input.text
	}
	clear := ""
	clearWidth := 0
	if !focused && actualQuery != "" {
		clear = buttonStyle("Clear", false)
		clearWidth = 2 + ansi.StringWidth(clear)
	}
	queryWidth := max(1, m.width-4-2-ansi.StringWidth(suffix)-clearWidth)
	if focused {
		query = m.renderedInput(queryWidth)
	} else {
		query = truncate(query, queryWidth)
	}
	prompt := "/ " + query
	if focused {
		prompt = ink(colorAccent, "/ ") + ink(colorBright, query)
	} else if actualQuery == "" {
		prompt = ink(colorMuted, prompt)
	}
	text := prompt + suffix
	l := textLine(text)
	l.hits = []hit{{x: 0, w: ansi.StringWidth(text), action: "filter"}}
	if clear != "" {
		l.text += "  " + clear
		l.hits = append(l.hits, hit{x: ansi.StringWidth(text) + 2, w: ansi.StringWidth(clear), action: "clear"})
	}
	return l
}
func (m *model) sandboxLines() []line {
	rows := m.visibleRows()
	lines := []line{m.filterLine(len(m.rows), len(rows), "sandboxes"), textLine("")}
	columns := m.sandboxColumns(rows)
	lines = append(lines, textLine(ink(colorMuted, columns.header())))
	if m.loading {
		return append(lines, textLine("Loading sandboxes…"))
	}
	if len(rows) == 0 {
		message := "No sandboxes found"
		for _, e := range m.errors {
			if e.code == store.CodeSandboxInventory {
				message = "Inventory unavailable · c Retry · s Status details"
			}
		}
		if m.queries[m.page] != "" {
			message = "No sandboxes match “" + m.queries[m.page] + "”"
		}
		return append(lines, textLine(message))
	}
	for _, r := range rows {
		id := r.Sandbox.ID
		if m.expanded == id {
			lines = append(lines, m.sandboxDetails(r)...)
			continue
		}
		selected := id == m.selected
		prefix := "  "
		if selected {
			prefix = "> "
		}
		label := r.Label
		if m.input != nil && m.input.kind == "rename" && m.input.target == id {
			label = m.renderedInput(columns.label)
		}
		status := statusStyle(r.Status.Connection, statusDescription(r))
		if r.AssociationIssue != "" || r.Status.Stale {
			status = ink(colorWarning, statusDescription(r))
		}
		if m.pending[id] {
			status = m.sandboxProgress(id)
		}
		mark := enabledMark(r)
		if choice, ok := m.pendingEnabled[id]; ok && choice == store.Enabled {
			mark = ink(colorAccent, "✓")
		}
		text := columns.render(prefix+"  ", mark, highlight(m.queries["Sandboxes"], safeText(r.Sandbox.Name)), ink(colorSand, highlight(m.queries["Sandboxes"], safeText(label))), status)
		l := textLine(text)
		l.hits = []hit{{x: 4, w: columns.enabled, action: "enabled", id: id}, {x: 5 + columns.enabled, w: max(1, m.width-9-columns.enabled), action: "select", id: id}, {x: 2, w: 2, action: "select", id: id}}
		lines = append(lines, l)

	}
	return lines
}
func onOff(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}
func (m *model) pluginLines() []line {
	plugins := m.visiblePlugins()
	nameWidth := min(34, max(12, m.width-39))
	syncWidth := 12
	if m.width < 60 {
		nameWidth, syncWidth = max(1, m.width-4-2-1-4), 4
	}
	heading := "  " + padded("PLUGIN", nameWidth) + " " + padded("SYNC", syncWidth) + " AVAILABILITY"
	if m.width < 60 {
		heading = "  " + padded("PLUGIN", nameWidth) + " SYNC"
	}
	lines := []line{m.filterLine(len(m.plugins), len(plugins), "plugins"), textLine(ink(colorMuted, heading))}
	for i, p := range plugins {
		prefix := "  "
		if i == m.pluginIndex {
			prefix = "> "
		}
		available := "✓ Installed locally"
		availabilityColor := colorSuccess
		if !p.Installed {
			available = "! Not installed locally"
			availabilityColor = colorWarning
		}
		if p.UnsupportedReason != "" {
			available = "✗ Unsupported"
			availabilityColor = colorError
		}
		l := textLine(prefix + padded(highlight(m.queries["Plugins"], p.ID), nameWidth) + " " + padded(enabledStyle(p.Enabled, onOff(p.Enabled)), syncWidth) + " " + ink(availabilityColor, available))
		choiceX := 2 + nameWidth + 1
		l.hits = []hit{{x: 0, w: choiceX, action: "plugin", id: p.ID}, {x: choiceX, w: syncWidth, action: "default", id: p.ID}}
		if m.width < 60 {
			l.text = prefix + padded(highlight(m.queries["Plugins"], p.ID), nameWidth) + " " + padded(enabledStyle(p.Enabled, onOff(p.Enabled)), syncWidth)
		}
		lines = append(lines, l)
		if m.width < 60 {
			lines = append(lines, textLine("  "+ink(availabilityColor, available)))
		}
		if p.UnsupportedReason != "" {
			for _, part := range strings.Split(ansi.Hardwrap(safeText(p.UnsupportedReason), max(10, m.width-8), true), "\n") {
				lines = append(lines, textLine("    "+ink(colorWarning, part)))
			}
		}
	}
	if len(plugins) == 0 {
		message := "No herdr plugins match this filter"
		if len(m.plugins) == 0 {
			message = "No herdr plugins available"
		}
		return append(lines, textLine(message))
	}
	explanation := "This herdr plugin will not be synchronized. Existing copies stay intact."
	if plugins[min(m.pluginIndex, len(plugins)-1)].Enabled {
		explanation = "The matching herdr plugin version will be copied to enabled sandboxes while Management is ON."
	}
	lines = append(lines, textLine(""))
	for _, part := range strings.Split(ansi.Hardwrap(explanation, max(10, m.width-4), true), "\n") {
		lines = append(lines, textLine(part))
	}
	return lines
}
func (m *model) statusActions() []detailAction {
	return m.statusActionsAt(time.Now())
}
func (m *model) statusActionsAt(now time.Time) []detailAction {
	actions := []detailAction{{"toggle", "Management"}}
	sshReady := false
	for _, check := range m.checks.Checks {
		if check.Name == "Sandbox SSH" {
			sshReady = check.Level == precheck.Ready
		}
	}
	if !sshReady {
		actions = append(actions, detailAction{"ssh", "Set up SSH…"})
	}
	if !m.workerEnabled || m.worker.State != "running" || m.worker.LastSync.IsZero() || now.Sub(m.worker.LastSync) > 30*time.Second || m.hasBlockingErrors() {
		actions = append(actions, detailAction{"refresh", "Refresh"})
	}
	return actions
}
func (m *model) statusLines() []line {
	actions := m.statusActions()
	on, off := enabledStyle(m.workerEnabled, "ON"), enabledStyle(!m.workerEnabled, "OFF")
	if m.workerControlError {
		if m.workerEnabled {
			on = ink(colorError, "ON")
		} else {
			off = ink(colorError, "OFF")
		}
	}
	toggle := textLine(on + ink(colorMuted, " | ") + off + "  " + m.managementStatus())
	toggle.hits = []hit{{x: 0, w: 2, action: "status-choice", id: "on"}, {x: 5, w: 3, action: "status-choice", id: "off"}, {x: 9, w: ansi.StringWidth(m.managementStatus()), action: "status-action", id: "0"}}
	border, title := colorSeparator, "Management"
	if m.focus == "table" && m.button == 0 {
		border, title = colorText, "Management ›"
	}
	lines := outline([]line{toggle}, title, m.width-4, 0, border)
	var refresh line
	for i, action := range actions[1:] {
		label := action.label
		if i+1 == m.button {
			label = "› " + label
		}
		control := controlLine([]string{label}, []string{"status-action"}, []string{fmt.Sprint(i + 1)})
		if action.id == "refresh" {
			refresh = control
		} else {
			lines = append(lines, control)
		}
	}

	lines = append(lines, textLine(""), textLine(ink(colorMuted, padded("PREREQUISITE", 26)+padded("RESULT", 14)+"STATUS")))
	for _, c := range statusPrerequisites(m.checks.Checks) {
		color := colorWarning
		if c.Level == precheck.Ready {
			color = colorSuccess
		} else if c.Level == precheck.Blocked {
			color = colorError
		}
		lines = append(lines, textLine(padded(productText(c.Name), 26)+padded(ink(color, symbol(c.Level)), 14)+safeText(productText(c.Detail))))
		if c.Action != "" {
			lines = append(lines, textLine(ink(colorInfo, "  Next: "+safeText(productText(c.Action)))))
		}
	}
	if refresh.text != "" {
		lines = append(lines, textLine(""), refresh)
	}
	return lines
}
func diagnosticColor(code string) string {
	if store.SeverityForCode(code) == store.SeverityWarning {
		return colorWarning
	}
	return colorError
}

func (m *model) hasBlockingErrors() bool {
	for _, e := range m.errors {
		if store.SeverityForCode(e.code) != store.SeverityWarning {
			return true
		}
	}
	return false
}

func (m *model) diagnosticTitle() string {
	warnings, newCount, allCount := 0, 0, 0
	groups := m.diagnosticGroups()
	for _, e := range groups {
		if store.SeverityForCode(e.code) == store.SeverityWarning {
			warnings++
		}
		allCount += e.count
		if !e.dismissed {
			newCount += e.count
		}
	}
	title := "Errors"
	if warnings == len(groups) {
		title = "Warnings"
	} else if warnings > 0 {
		title = "Errors and warnings"
	}
	newLabel, allLabel := fmt.Sprintf("New (%d)", newCount), fmt.Sprintf("All (%d)", allCount)
	newColor, allColor := colorAccent, colorMuted
	if m.showAllErrors {
		newColor, allColor = colorMuted, colorAccent
	}
	if colorsEnabled() {
		if m.showAllErrors {
			allLabel = "\x1b[1m" + allLabel + "\x1b[22m"
		} else {
			newLabel = "\x1b[1m" + newLabel + "\x1b[22m"
		}
	}
	titleColor := colorSeparator
	if m.focus == "diagnostics" {
		titleColor = colorText
	}
	title = ink(titleColor, title) + " · " + ink(newColor, newLabel) + " | " + ink(allColor, allLabel)
	if !m.showAllErrors && newCount < allCount {
		title += fmt.Sprintf(" · %d active hidden", allCount-newCount)
	}
	if m.focus == "diagnostics" {
		title = "> " + title
	}
	return title
}

func (m *model) errorLines() []line {
	entries := m.visibleErrors()
	if len(entries) == 0 {
		lines := []line{textLine("")}
		for _, part := range wrapHint("No new diagnostics. Switch to All to see dismissed active entries.", max(1, m.width-6)) {
			lines = append(lines, textLine("  "+ink(colorMuted, part)))
		}
		return append(lines, textLine(""))
	}
	var lines []line
	for i, e := range entries {
		code := e.code
		if code == "" {
			code = "Unknown code"
		}
		prefix := "  "
		if m.focus == "errors" && i == m.errorIndex {
			prefix = "> "
		}
		color := diagnosticColor(e.code)
		header := ink(color, code) + " · " + m.diagnosticScope(e.scope, "") + diagnosticTime(e.seen)
		if e.count > 1 {
			header += " " + ink(colorMuted, fmt.Sprintf("×%d", e.count))
		}
		lines = append(lines, textLine(""))
		for _, part := range strings.Split(ansi.Wrap(header, max(1, m.width-6), ""), "\n") {
			l := textLine(prefix + part)
			l.hits = []hit{{x: 0, w: m.width, action: "error", id: fmt.Sprint(i)}}
			lines = append(lines, l)
			prefix = "  "
		}
		for childIndex, e := range e.entries {
			if childIndex > 0 {
				lines = append(lines, textLine(""))
			}
			if e.pluginID != "" {
				name := safeText(e.pluginID)
				if colorsEnabled() {
					name = "\x1b[1m" + name + "\x1b[22m"
				}
				for _, part := range strings.Split(ansi.Wrap(name, max(1, m.width-6), ""), "\n") {
					l := textLine("  " + part)
					l.hits = []hit{{x: 0, w: m.width, action: "error", id: fmt.Sprint(i)}}
					lines = append(lines, l)
				}
			}
			detail := safeText(productText(e.detail))
			if e.code == store.CodeHerdrCompatibility {
				for _, row := range m.rows {
					if e.scope == row.Sandbox.ID || e.scope == row.Scope || e.scope == store.StatusKey(row.Scope, row.Sandbox.ID) {
						detail = m.herdrVersionContext(row) + " " + detail
						break
					}
				}
			}
			if e.ref != "" {
				detail = ink(colorMuted, safeText(e.ref)) + " " + detail
			}
			for _, part := range strings.Split(ansi.Wrap(detail, max(1, m.width-6), ""), "\n") {
				l := textLine("  " + part)
				l.hits = []hit{{x: 0, w: m.width, action: "error", id: fmt.Sprint(i)}}
				lines = append(lines, l)
			}
			lines = append(lines, m.repairCallout(m.diagnosticScope(e.scope, ""), e.recovery, e.command)...)
			lines = append(lines, m.buildOutput(e.output)...)
		}
		if e.dismissed {
			lines = append(lines, textLine("  "+ink(colorMuted, "Dismissed · still active")))
		} else if e.firstSeen.IsZero() {
			lines = append(lines, textLine("  "+ink(colorMuted, "Saving diagnostic…")))
		}
	}
	return lines
}

func (m *model) buildOutput(output string) []line {
	if output == "" {
		return nil
	}
	lines := []line{textLine("  " + ink(colorMuted, "Build output:"))}
	for _, part := range strings.Split(ansi.Wrap(safeText(output), max(1, m.width-6), ""), "\n") {
		lines = append(lines, textLine("  "+ink(colorMuted, part)))
	}
	return lines
}

// Repair advice stays above build output so the next step is visible first.
func (m *model) repairCallout(sandbox, recovery, command string) []line {
	if recovery == "" && command == "" {
		return nil
	}
	width := max(1, m.width-10)
	prefix := "  " + ink(colorAccent, "│") + "  "
	lines := []line{textLine(""), textLine(prefix)}
	if recovery != "" {
		for _, part := range wrapHint(safeText(productText(recovery)), width) {
			lines = append(lines, textLine(prefix+ink(colorAccent, part)))
		}
	}
	if command != "" {
		for _, part := range wrapHint("Run inside "+sandbox+":", width) {
			lines = append(lines, textLine(prefix+ink(colorAccent, part)))
		}
		lines = append(lines, textLine(prefix))
		for _, part := range wrapHint(safeText(command), width) {
			lines = append(lines, textLine(prefix+ink(colorBright, part)))
		}
		lines = append(lines, textLine(prefix))
	}
	return append(lines, textLine(prefix), textLine(""))
}

func (m *model) eventLines() []line {
	var lines []line

	for i := len(m.events) - 1; i >= 0; i-- {
		e := m.events[i]
		color := diagnosticColor(e.Code)
		if store.SeverityForCode(e.Code) == store.SeverityInfo {
			color = colorInfo
		}
		if e.Resolved {
			color = colorSuccess
		}
		lines = append(lines, textLine(""))
		header := ink(color, safeText(e.Code)) + " · " + m.diagnosticScope(e.Scope, e.Name) + diagnosticTime(e.At)
		if e.Ref != "" {
			header = ink(colorMuted, safeText(e.Ref)) + " " + header
		}
		if m.focus == "events" && m.eventIndex == len(m.events)-1-i {
			header = "> " + header
		}
		if e.PluginID != "" {
			header += " · " + safeText(e.PluginID)
		}
		for _, part := range strings.Split(ansi.Wrap(header, max(1, m.width-4), ""), "\n") {
			l := textLine(part)
			l.hits = []hit{{x: 0, w: m.width, action: "event", id: fmt.Sprint(len(m.events) - 1 - i)}}
			lines = append(lines, l)
		}
		if !e.Resolved {
			lines = append(lines, m.repairCallout(m.diagnosticScope(e.Scope, e.Name), e.Recovery, e.Command)...)
		}
		for _, part := range strings.Split(ansi.Wrap(safeText(productText(e.Detail)), max(10, m.width-6), ""), "\n") {
			lines = append(lines, textLine("  "+part))
		}
		lines = append(lines, m.buildOutput(e.Output)...)
	}
	return lines
}

func (m *model) diagnosticScope(scope, name string) string {
	for _, row := range m.rows {
		if row.Sandbox.ID == scope || store.StatusKey(row.Scope, row.Sandbox.ID) == scope {
			return safeText(row.Sandbox.Name)
		}
	}
	if name != "" {
		return safeText(name)
	}
	if _, id, scoped := strings.Cut(scope, "/"); scoped {
		return "Unknown sandbox (" + truncate(safeText(id), 12) + ")"
	}
	return safeText(scope)
}

// fitLines keeps text and control bounds in the same layout at narrow widths.
func fitLines(lines []line, width int) []line {
	var result []line
	for _, l := range lines {
		if len(l.hits) > 0 && ansi.StringWidth(l.text) > width && (l.hits[0].action == "dialog" || l.hits[0].action == "action") {
			for _, h := range l.hits {
				text := ansi.Cut(l.text, h.x, h.x+h.w)
				h.x = 0
				for _, part := range strings.Split(ansi.Hardwrap(text, max(1, width), true), "\n") {
					h.w = ansi.StringWidth(part)
					result = append(result, line{text: part, hits: []hit{h}})
				}
			}
			continue
		}
		if len(l.hits) == 0 {
			for _, text := range strings.Split(ansi.Hardwrap(l.text, max(1, width), true), "\n") {
				result = append(result, textLine(text))
			}
		} else {
			result = append(result, l)
		}
	}
	return result
}
func (m *model) actionSpinner() string {
	return []string{"◐", "◓", "◑", "◒"}[m.ticks%4]
}

func (m *model) dialogLines() []line {
	switch m.dialog {
	case "startup":
		return []line{textLine("Start Management?"), textLine("Management sets up sandbox entries and synchronizes selected herdr plugins."), textLine(""), m.buttons([]string{"Keep OFF", "Start Management"})}

	case "ssh":
		return []line{textLine("Set up sandbox SSH?"), textLine("The setup process will temporarily use this terminal."), textLine("Follow its prompts; the interface returns when it finishes."), m.buttons([]string{"Cancel", "Start SSH setup"})}
	case "fix-herdr":
		r, _ := findRow(m.rows, m.dialogTarget)
		lines := []line{}
		for _, part := range wrapHint("Fix herdr in "+safeText(r.Sandbox.Name)+"? "+m.fixHerdrExplanationTo(r, m.dialogReason), max(1, m.width-4)) {
			lines = append(lines, textLine(part))
		}
		return append(lines, m.buttons([]string{"Cancel", "Fix herdr"}))

	}
	return nil
}
func (m *model) dialogButtonCount() int { return 2 }
func highlight(query, value string) string {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || query == "" {
		return value
	}
	q := []rune(strings.ToLower(query))
	i := 0
	var b strings.Builder
	for _, r := range value {
		if i < len(q) && []rune(strings.ToLower(string(r)))[0] == q[i] {
			b.WriteString("\x1b[4m")
			b.WriteRune(r)
			b.WriteString("\x1b[24m")
			i++
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
