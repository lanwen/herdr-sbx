package panel

import (
	"fmt"

	"github.com/lanwen/herdr-sbx/internal/store"
)

func (m *model) observationsFocused() bool {
	return m.focus == "errors" || m.focus == "diagnostics" || m.focus == "events"
}

func (m *model) observationBudget(available int) int {
	if (m.dialog != "" && m.dialog != "fix-herdr") || (len(m.pageErrors()) == 0 && len(m.pageEvents()) == 0) {
		return 0
	}
	if m.dialog == "fix-herdr" {
		return 1
	}
	budget, minimumMain := 1, 3
	if available >= 15 {
		budget, minimumMain = 3, 9
	} else if available >= 9 {
		budget, minimumMain = 2, 4
	}
	if m.observationsFocused() {
		feedback := 0
		if content := m.hintContent(false); len(content) > 0 {
			feedback = min(3, len(content))
			if m.width >= 60 && m.height >= 20 {
				feedback = min(3, len(content)+2)
			}
		}
		budget = max(budget, available-minimumMain-feedback)
	}
	return budget
}

func (m *model) pendingActionLabel() string {
	for target, label := range m.pendingActions {
		if m.pending[target] && m.operationInContext(target) {
			return label
		}
	}
	return ""
}

func (m *model) fixedHint(content []line, capacity int) []line {
	if m.dialog == "fix-herdr" {
		return m.fixedHerdrConfirmation(capacity)
	}
	compact := m.quitUntil.IsZero() && m.dialog == "" && (m.width < 60 || m.height < 20)
	m.hintCapacity = max(1, capacity-2)
	if compact {
		m.hintCapacity = max(1, capacity)
	}
	if len(content) == 0 || capacity < 1 {
		m.hintScrollable = false
		m.hintScroll = 0
		return nil
	}
	m.hintScrollable = len(content) > m.hintCapacity
	if !m.hintScrollable {
		m.hintScroll = 0
	}

	m.hintScroll = min(max(0, len(content)-m.hintCapacity), m.hintScroll)
	end := min(len(content), m.hintScroll+m.hintCapacity)
	visible := append([]line(nil), content[m.hintScroll:end]...)
	if compact {
		if m.hintScrollable && len(visible) > 0 {
			visible[len(visible)-1].text += " " + ink(colorMuted, "…")
		}
		return visible
	}
	title := "Hint"
	if m.hintScrollable {
		title += " …"
	}
	return outline(visible, title, m.width, 0)
}

func (m *model) fixedObservations(capacity int) []line {
	m.observationCapacity = capacity
	if capacity == 0 {
		return nil
	}
	if !m.observationsFocused() {
		return m.observationSummary(capacity)
	}
	content := m.observationSections(nil)
	previous := m.scroll
	m.scroll = m.observationScroll
	m.revealSelection(content, capacity)
	m.observationScroll = min(max(0, len(content)-capacity), m.scroll)
	m.scroll = previous
	end := min(len(content), m.observationScroll+capacity)
	return content[m.observationScroll:end]
}

func (m *model) observationDelta(delta int) int {
	step := min(max(1, m.observationCapacity-1), max(1, abs(delta)))
	if delta < 0 {
		return -step
	}
	return step
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *model) observationSummary(capacity int) []line {
	var result []line
	groups, events := m.visibleErrors(), m.pageEvents()
	if capacity == 1 && len(m.pageErrors()) > 0 && len(events) > 0 {
		_, newCount, allCount := m.diagnosticCounts()
		prefix := "Errors"
		if len(groups) > 0 {
			prefix = ink(diagnosticColor(groups[0].code), groups[0].code)
		}
		return []line{{text: truncate(prefix+fmt.Sprintf(" New%d/All%d Events%d", newCount, allCount, len(events)), m.width), hits: []hit{{w: m.width, action: "diagnostic-toggle"}}}}
	}
	if len(events) > 0 {
		latest := events[len(events)-1]
		title := m.eventTitle()
		if colorsEnabled() {
			title = colorSeparator + title + colorSeparator
		}
		text := title + ink(colorText, ": "+safeText(latest.Detail)+" · "+m.diagnosticScope(latest.Scope, latest.Name))
		result = append(result, line{text: truncate(text, m.width), hits: []hit{{w: m.width, action: "event-section"}}})
	}
	for index, group := range groups {
		if len(result) >= capacity {
			break
		}
		result = append(result, m.diagnosticSummaryLine(group, index))
	}
	if len(groups) == 0 && len(m.pageErrors()) > 0 && len(result) < capacity {
		result = append(result, line{text: truncate(m.diagnosticTitle(), m.width), hits: []hit{{w: m.width, action: "diagnostic-toggle"}}})
	}
	return result
}

func (m *model) diagnosticSummaryLine(group diagnosticGroup, index int) line {
	_, newCount, allCount := m.diagnosticCounts()
	caption := "Errors"
	if store.SeverityForCode(group.code) == store.SeverityWarning {
		caption = "Warnings"
	}
	text := ink(diagnosticColor(group.code), group.code) + " · " + m.diagnosticScope(group.scope, "") + fmt.Sprintf(" · %s New%d/All%d: ", caption, newCount, allCount) + safeText(group.detail)
	if m.width < 60 {
		text = ink(diagnosticColor(group.code), group.code) + fmt.Sprintf(" New%d/All%d: ", newCount, allCount) + safeText(group.detail)
	}
	return line{text: truncate(text, m.width), hits: []hit{{w: m.width, action: "error", id: fmt.Sprint(index)}}}
}
