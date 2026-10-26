package panel

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// sandboxColumns is shared by heading, rows, inline editing and mouse targets.
type sandboxColumns struct{ enabled, name, label, status int }

func (m *model) sandboxColumns(rows []registration.SandboxRow) sandboxColumns {
	c := sandboxColumns{enabled: 7, name: 7, label: 11, status: 9}
	if m.width < 72 {
		c.status = 0
	}
	if m.width < 60 {
		c.enabled, c.label = 1, 5
	}
	for _, r := range rows {
		m.growSandboxColumns(&c, r)
	}
	gaps := 2
	if c.status > 0 {
		gaps++
	}
	available := max(2, m.width-4-4-c.enabled-gaps-c.status)
	// Retain a useful label cell, shortening it before the sandbox name.
	c.name = min(c.name, max(1, available-min(c.label, 5)))
	c.label = max(1, available-c.name)
	return c
}
func (c sandboxColumns) render(prefix, enabled, name, label, status string) string {
	text := prefix + padded(enabled, c.enabled) + " " + padded(name, c.name) + " " + padded(label, c.label)
	if c.status > 0 {
		text += " " + padded(status, c.status)
	}
	return text
}
func (c sandboxColumns) header() string {
	enabled, label := "ENABLED", "HERDR LABEL"
	if c.enabled == 1 {
		enabled, label = "E", "HERDR"
	}
	return c.render("    ", enabled, "SANDBOX", label, "STATUS")
}
func enabledMark(r registration.SandboxRow) string {
	if r.EntryPresent && r.Enabled == store.Enabled {
		return ink(colorAccent, "✓")
	}
	return ink(colorMuted, "·")
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
func statusDescription(r registration.SandboxRow) string {
	if r.Status.Stale {
		return "? Stale observation"
	}
	if r.AssociationIssue != "" {
		return "Entry ambiguous"
	}
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

func (m *model) growSandboxColumns(c *sandboxColumns, r registration.SandboxRow) {
	if c.status > 0 {
		c.status = max(c.status, min(24, ansi.StringWidth(statusDescription(r))))
		if m.pending[r.Sandbox.ID] {
			c.status = max(c.status, ansi.StringWidth(m.sandboxProgress(r.Sandbox.ID)))
		}
	}
	c.name = max(c.name, ansi.StringWidth(safeText(r.Sandbox.Name)))
	label := r.Label
	if m.input != nil && m.input.kind == "rename" && m.input.target == r.Sandbox.ID {
		label = m.input.text
	}
	c.label = max(c.label, ansi.StringWidth(safeText(label)))
}
