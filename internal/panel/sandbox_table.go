package panel

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// sandboxColumns describes terminal cells inside the surrounding section.
// One calculation serves every row, heading, and inline label editor.
type sandboxColumns struct {
	management, name, runtime, label, readiness int
	compactRuntime, compactManagement           bool
}

func (m *model) sandboxColumns(rows []registration.SandboxRow) sandboxColumns {
	c := sandboxColumns{management: 10, name: 7, runtime: 7, label: 11, readiness: 9}
	for _, r := range rows {
		c.name = max(c.name, ansi.StringWidth(safeText(r.Sandbox.Name)))
		label := r.Label
		if m.input != nil && m.input.kind == "rename" && m.input.target == r.Sandbox.ID {
			label = m.input.text
		}
		c.label = max(c.label, ansi.StringWidth(safeText(label)))
		c.runtime = max(c.runtime, ansi.StringWidth(runtimeDescription(r)))
		c.readiness = max(c.readiness, min(22, ansi.StringWidth(readinessDescription(r))))
	}
	if m.width < 72 {
		c.readiness = 0
	}
	if m.width < 60 {
		c.management, c.compactManagement = 2, true
		c.runtime, c.compactRuntime = 1, true
	}
	// Outer outline, selection/disclosure prefix, and inter-column spaces.
	gaps := 4
	if c.readiness == 0 {
		gaps--
	}
	available := max(2, m.width-4-4-gaps-c.management)
	if !c.compactManagement && c.name+c.label+c.runtime+c.readiness > available {
		available += c.management - 2
		c.management, c.compactManagement = 2, true
	}
	if c.name+c.label+c.runtime+c.readiness > available {
		c.compactRuntime = true
		c.runtime = 1
	}
	c.readiness = min(c.readiness, max(0, available-c.runtime-14))
	remaining := max(2, available-c.runtime-c.readiness)
	// Preserve the sandbox name first, leaving a useful label cell beside it.
	c.name = min(c.name, max(1, remaining-min(c.label, 7)))
	c.label = remaining - c.name
	return c
}
func (c sandboxColumns) render(prefix, management, name, runtime, label, readiness string) string {
	text := prefix + padded(management, c.management) + " " + padded(name, c.name) + " " + padded(runtime, c.runtime) + " " + padded(label, c.label)
	if c.readiness > 0 {
		text += " " + padded(readiness, c.readiness)
	}
	return text
}
func (c sandboxColumns) managementText(choice store.ManagementChoice) string {
	if c.compactManagement {
		switch choice {
		case store.Watched:
			return "W"
		case store.Unmanaged:
			return "UM"
		case store.Unwatched:
			return "UW"
		case store.Disabled:
			return "D"
		case store.Untracked:
			return "UT"
		default:
			return "?"
		}
	}
	text := string(choice)
	return strings.ToUpper(text[:min(1, len(text))]) + text[min(1, len(text)):]
}
func (c sandboxColumns) header() string {
	runtime, management, label := "RUNTIME", "MANAGEMENT", "HERDR LABEL"
	if c.compactRuntime {
		runtime = "S"
	}
	if c.compactManagement {
		management, label = "M", "HERDR"
	}
	return c.render("    ", management, "SANDBOX", runtime, label, "READINESS")
}
func runtimeDescription(r registration.SandboxRow) string {
	state := safeText(r.Sandbox.Status)
	if state == "" {
		state = "? Unknown"
	}
	if r.Status.Stale {
		state += " (stale)"
	}
	return state
}
func readinessDescription(r registration.SandboxRow) string {
	if r.Status.Connection == "" {
		return "? Unknown"
	}
	return safeText(r.Status.Connection)
}
func runtimeDot(state string) string {
	if colorsEnabled() {
		return stateDot(state)
	}
	switch strings.ToLower(state) {
	case "running":
		return "●"
	case "stopped", "exited":
		return "○"
	case "pending", "starting", "stopping", "creating":
		return "…"
	case "failed", "error":
		return "✗"
	default:
		return "?"
	}
}
