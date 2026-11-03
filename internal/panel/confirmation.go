package panel

import (
	"strings"

	"github.com/lanwen/herdr-sbx/internal/version"
)

// Consent stays fixed while its longer explanation scrolls independently.
func (m *model) fixedHerdrConfirmation(capacity int) []line {
	compact := m.width < 60 || m.height < 20
	width, chrome := max(1, m.width-4), 2
	if compact {
		width, chrome = m.width, 0
	}
	summary, explanation, controls := m.herdrConfirmationParts(width, compact)
	m.hintCapacity = max(1, capacity-chrome-len(summary)-len(controls))
	m.hintScrollable = len(explanation) > m.hintCapacity
	m.hintScroll = min(max(0, len(explanation)-m.hintCapacity), m.hintScroll)
	end := min(len(explanation), m.hintScroll+m.hintCapacity)
	visible := append([]line(nil), summary...)
	body := append([]line(nil), explanation[m.hintScroll:end]...)
	if m.hintScrollable && compact && len(visible) > 0 {
		visible[0].text += ink(colorMuted, " …")
	}
	visible = append(visible, body...)
	visible = append(visible, controls...)
	if compact {
		return visible
	}
	title := "Hint"
	if m.hintScrollable {
		title += " …"
	}
	return outline(visible, title, m.width, 0)
}

func (m *model) herdrConfirmationParts(width int, compact bool) ([]line, []line, []line) {
	row, _ := findRow(m.rows, m.dialogTarget)
	install := "Install herdr "
	downgrade := false
	if observed := row.Status.GuestServer; observed != nil {
		if guest := version.Parse(observed.Version); guest != "" && version.Compare(guest, m.dialogReason) > 0 {
			install = "Downgrade herdr to "
			downgrade = true
		}
	}
	var summary []line
	if compact {
		action := "Fix herdr to " + m.dialogReason
		if downgrade {
			action = install + m.dialogReason
		}
		summary = append(summary, textLine(action))
		for _, part := range strings.Split(styledHardwrap(safeText(row.Sandbox.Name), width), "\n") {
			summary = append(summary, textLine(part))
		}
	} else {
		summary = append(summary, textLine("Fix herdr in "+safeText(row.Sandbox.Name)+"?"), textLine(install+m.dialogReason))
	}
	summary = fitLines(summary, width)
	handoff := "Handoff keeps panes; no forced restart."
	if width < 38 {
		handoff = "Handoff; no forced restart."
	}
	summary = append(summary, textLine(handoff))
	var explanation []line
	for _, part := range wrapHint(m.fixHerdrExplanationTo(row, m.dialogReason), width) {
		explanation = append(explanation, textLine(part))
	}
	controls := fitLines([]line{m.buttons([]string{"Cancel", "Fix herdr"})}, width)
	return summary, explanation, controls
}
