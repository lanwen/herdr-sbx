package panel

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

func (m *model) sandboxDetails(r registration.SandboxRow) []line {
	id := r.Sandbox.ID
	label := r.Label
	if label == "" {
		label = "—"
	}
	lines := []line{textLine(safeText(r.Sandbox.Name) + " → " + ink(colorSand, safeText(label)) + " (" + readinessStyle(r.Status.Connection, readinessDescription(r)) + ")"), textLine("")}
	ownership := "Unowned"
	if r.Owned {
		ownership = "Owned"
	}
	lines = append(lines, m.detailPluginLines(r)...)
	availableActions := m.sandboxActions(r)
	var actions []line
	capacity := max(1, m.width-10)
	for i, action := range availableActions {
		label := action.label
		focused := m.focus == "actions" && m.button%len(availableActions) == i
		if m.dialog == "fix-herdr" {
			focused = action.id == "fix-herdr"
		}
		if focused {
			label = "› " + label
		}
		button := controlLine([]string{label}, []string{"action"}, []string{action.id})
		if action.id == "adopt" {
			button.text = primaryButtonStyle(label, focused)
			button.hits[0].w = ansi.StringWidth(button.text)
		}
		if len(actions) == 0 || ansi.StringWidth(actions[len(actions)-1].text)+2+ansi.StringWidth(button.text) > capacity {
			actions = append(actions, button)
			continue
		}
		row := &actions[len(actions)-1]
		x := ansi.StringWidth(row.text) + 2
		row.text += "  " + button.text
		for _, h := range button.hits {
			h.x += x
			row.hits = append(row.hits, h)
		}
	}
	lines = append(lines, fitLines(actions, capacity)...)
	title := "Details · " + safeText(id) + " · " + ownership
	result := outline(lines, title, m.width-4, 2)
	if len(result) > 0 {
		observed := detailsObservationAge(r.Status.ObservedAt, time.Now())
		runtime := runtimeDescription(r)
		if m.width < 60 {
			runtime = r.Sandbox.Status
			observed = ""
		}
		result[0] = textLine(detailBorder(title, observed, runtimeBadge(r.Sandbox.Status, runtime), m.width-4, 2))
		if server := m.guestServerLabel(r); server != "" && ansi.StringWidth(server) <= m.width-12 {
			fill := m.width - ansi.StringWidth(server) - 11
			result[len(result)-1] = textLine(ink(colorSeparator, "  ╰"+strings.Repeat("─", fill)+" "+server+" ─╯"))
		}
	}
	return result
}

func guestServerDescription(r registration.SandboxRow) string {
	observed := r.Status.GuestServer
	if observed == nil || observed.Version == "" {
		return ""
	}
	return "herdr " + safeText(observed.Version)
}

func detailsObservationAge(observedAt, now time.Time) string {
	if observedAt.IsZero() || now.Sub(observedAt) <= 2*time.Minute {
		return ""
	}
	return relativeTime(observedAt)
}

type detailAction struct {
	id, label string
}

func (m *model) sandboxActions(r registration.SandboxRow) []detailAction {
	switch r.Management {
	case store.Unmanaged:
		return []detailAction{{"adopt", "Adopt"}}
	case store.Disabled, store.Untracked:
		return []detailAction{{"watch", "Watch"}}
	}
	actions := make([]detailAction, 0, 5)
	if r.Management == store.Watched {
		actions = append(actions, detailAction{"unwatch", "Unwatch"})
	} else {
		actions = append(actions, detailAction{"watch", "Watch"})
	}
	if r.Owned {
		actions = append(actions, detailAction{"disable", "Disable · remove from herdr"})
	}
	if r.Management == store.Watched && strings.EqualFold(r.Sandbox.Status, "running") {
		needsResync := !strings.EqualFold(r.Status.Connection, "ready")
		for _, e := range m.errors {
			if (e.code == store.CodePreparation || store.IsGuestPluginCode(e.code)) && (e.scope == r.Sandbox.ID || e.scope == r.Scope || e.scope == store.StatusKey(r.Scope, r.Sandbox.ID)) {
				needsResync = true
				break
			}
		}
		if needsResync {
			actions = append(actions, detailAction{"resync", "Re-sync"})
		}
	}
	if m.canFixHerdr(r) {
		actions = append(actions, detailAction{"fix-herdr", "Fix herdr"})
	}
	if r.Owned {
		actions = append(actions, detailAction{"rename", "Rename herdr label…"})
	}
	return actions
}

func (m *model) detailPluginLines(r registration.SandboxRow) []line {
	// Root section consumes four cells, Details six, and this outline four.
	inner := max(1, m.width-14)
	choiceWidth, effectiveWidth := 7, 11
	nameWidth := 6
	for _, p := range m.plugins {
		nameWidth = max(nameWidth, ansi.StringWidth(p.ID))
	}
	nameWidth = min(nameWidth, max(1, inner-2-choiceWidth-effectiveWidth-2))
	if m.width < 60 {
		nameWidth = max(1, inner-2)
	}
	render := func(prefix, name, choice, effective string) string {
		return prefix + padded(name, nameWidth) + " " + padded(choice, choiceWidth) + " " + padded(effective, effectiveWidth)
	}
	lines := []line{textLine(ink(colorMuted, render("  ", "PLUGIN", "CHOICE", "EFFECTIVE")))}
	if m.width < 60 {
		lines = nil
	}
	for i, p := range m.plugins {
		choice, loading := m.pluginChoiceDisplay(r.Scope, r.Sandbox.ID, p.ID)
		effective := m.settings.Effective(r.Scope, r.Sandbox.ID).Plugins[p.ID].Enabled
		prefix := "  "
		if m.focus == "plugins" && i == m.detailIndex {
			prefix = "> "
		}
		effectiveText := enabledStyle(effective, onOff(effective))
		if loading {
			effectiveText = ink(colorInfo, m.actionSpinner()+" Loading")
		} else if p.UnsupportedReason != "" {
			effectiveText = ink(colorError, "Unsupported")
		}
		l := textLine(render(prefix, p.ID, ink(colorInfo, choice), effectiveText))
		l.hits = []hit{{x: 0, w: inner, action: "override", id: p.ID}}
		if m.width < 60 {
			l.text = prefix + padded(p.ID, nameWidth)
		}
		lines = append(lines, l)
		if m.width < 60 {
			text := "  " + ink(colorInfo, choice) + " → " + effectiveText
			if ansi.StringWidth(text) > inner {
				lines = append(lines, line{text: "  " + ink(colorInfo, choice), hits: []hit{{x: 0, w: inner, action: "override", id: p.ID}}})
				text = "  → " + effectiveText
			}
			lines = append(lines, line{text: text, hits: []hit{{x: 0, w: inner, action: "override", id: p.ID}}})
		}
		if p.UnsupportedReason != "" {
			for _, part := range strings.Split(ansi.Hardwrap(safeText(p.UnsupportedReason), max(10, inner-2), true), "\n") {
				lines = append(lines, textLine("  "+ink(colorWarning, part)))
			}
		}
	}
	if len(m.plugins) == 0 {
		lines = append(lines, textLine(ink(colorMuted, "No herdr plugins available")))
	}
	return outline(lines, "Plugins", m.width-10, 0)
}
func runtimeBadge(state, text string) string {
	color := colorInfo
	switch strings.ToLower(state) {
	case "running":
		color = colorSuccess
	case "stopped", "exited":
		color = colorMuted
	case "pending", "starting", "stopping", "creating":
		color = colorWarning
	case "failed", "error":
		color = colorError
	}
	return badgeStyle(color, text)
}
func relativeTime(when time.Time) string {
	age := time.Since(when)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
	}
}
func detailBorder(title, observed, badge string, width, indent int) string {
	// Keep the status badge against the right corner, even with a long identity.
	if observed != "" {
		observed = " " + observed + " "
	}
	title = truncate(title, max(1, width-indent-ansi.StringWidth(observed)-ansi.StringWidth(badge)-6))
	left := strings.Repeat(" ", indent) + "╭─ " + title + " "
	fill := max(0, width-ansi.StringWidth(left)-ansi.StringWidth(observed)-ansi.StringWidth(badge)-2)
	return ink(colorSeparator, left+strings.Repeat("─", fill)) + ink(colorMuted, observed) + " " + badge + ink(colorSeparator, "╮")
}

func (m *model) hostHerdrVersion() string {
	for _, check := range m.checks.Checks {
		if strings.EqualFold(check.Name, "herdr") && check.Level == "ready" {
			return version.Parse(check.Detail)
		}
	}
	return ""
}
func (m *model) guestVersionComparison(r registration.SandboxRow) (int, bool) {
	host := m.hostHerdrVersion()
	if host == "" || r.Status.GuestServer == nil {
		return 0, false
	}
	guest := version.Parse(r.Status.GuestServer.Version)
	if guest == "" {
		return 0, false
	}
	return version.Compare(guest, host), true
}
func (m *model) guestServerLabel(r registration.SandboxRow) string {
	label := guestServerDescription(r)
	if order, known := m.guestVersionComparison(r); known {
		if order < 0 {
			label += " ↓"
		} else if order > 0 {
			label += " ↑"
		}
	}
	return label
}
func (m *model) canFixHerdr(r registration.SandboxRow) bool {
	if m.hostHerdrVersion() == "" || !r.Owned || r.Management != store.Watched || !strings.EqualFold(r.Sandbox.Status, "running") {
		return false
	}
	if order, known := m.guestVersionComparison(r); known && order != 0 {
		return true
	}
	return m.rowHasCode(r, store.CodeHerdrCompatibility)
}
func guestVersionHint(r registration.SandboxRow) string {
	observed := r.Status.GuestServer
	if observed == nil || observed.Version == "" {
		return "The herdr server version in this sandbox is unknown."
	}
	qualifier := "This sandbox runs herdr "
	if !observed.Current || r.Status.Stale || !strings.EqualFold(r.Sandbox.Status, "running") || time.Since(observed.CheckedAt) > 15*time.Minute {
		qualifier = "The last known herdr server version in this sandbox is "
	}
	return qualifier + safeText(observed.Version) + "."
}
func (m *model) fixHerdrExplanation(r registration.SandboxRow) string {
	return m.fixHerdrExplanationTo(r, m.hostHerdrVersion())
}
func (m *model) fixHerdrExplanationTo(r registration.SandboxRow, host string) string {
	action := "Fix herdr installs herdr " + host + " to match the host. This may downgrade the installed executable, which can differ from the last observed server version."
	if r.Status.GuestServer != nil {
		if guest := version.Parse(r.Status.GuestServer.Version); guest != "" && version.Compare(guest, host) > 0 {
			action = "Fix herdr downgrades this sandbox to herdr " + host + " to match the host."
		}
	}
	return guestVersionHint(r) + " " + action + " It requests server handoff to keep panes. A failed handoff does not force a restart."
}

func (m *model) rowHasCode(r registration.SandboxRow, code string) bool {
	for _, e := range m.errors {
		if e.code == code && (e.scope == r.Sandbox.ID || e.scope == r.Scope || e.scope == store.StatusKey(r.Scope, r.Sandbox.ID)) {
			return true
		}
	}
	return false
}
func (m *model) herdrVersionContext(r registration.SandboxRow) string {
	hint := guestVersionHint(r)
	if host := m.hostHerdrVersion(); host != "" {
		hint += " The host runs herdr " + host + "."
	}
	return hint
}
func (m *model) herdrCompatibilityHint(r registration.SandboxRow) string {
	hint := m.herdrVersionContext(r)
	if m.canFixHerdr(r) {
		hint += " Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."
	} else {
		hint += " Update herdr inside the sandbox with its original installation method, then choose Re-sync."
		if m.hostHerdrVersion() == "" {
			hint += " Refresh observations to check the host version before using Fix herdr."
		}
	}
	return hint
}
