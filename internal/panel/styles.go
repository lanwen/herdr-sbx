package panel

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
)

// These terminal colors echo the sandbox CLI palette without depending on its
// rendering code. Symbols and labels carry the same meaning without color.
const (
	colorBright        = "\x1b[38;2;229;242;252m"
	colorText          = "\x1b[38;2;192;192;192m"
	colorMuted         = "\x1b[38;2;128;128;128m"
	colorAccent        = "\x1b[38;2;122;162;247m"
	colorSuccess       = "\x1b[38;2;158;206;106m"
	colorError         = "\x1b[38;2;247;118;142m"
	colorWarning       = "\x1b[38;2;224;175;104m"
	colorSand          = "\x1b[38;2;216;195;165m" // Matches the Herdr sandbox machine prefix (#D8C3A5).
	colorInfo          = "\x1b[38;2;125;207;255m"
	colorSeparator     = "\x1b[38;2;55;61;86m"
	background         = "\x1b[48;2;28;28;34m"
	backgroundTab      = "\x1b[48;2;43;43;55m"
	backgroundSelected = "\x1b[48;2;61;90;158m"
	foregroundSelected = "\x1b[38;2;255;255;255m"
)

func colorsEnabled() bool { _, disabled := os.LookupEnv("NO_COLOR"); return !disabled }
func ink(color, text string) string {
	if !colorsEnabled() || text == "" {
		return text
	}
	return color + text + "\x1b[39m"
}
func filled(fg, bg, text string) string {
	if !colorsEnabled() {
		return text
	}
	// Restore the containing surface after nested semantic text or controls.
	text = strings.ReplaceAll(text, "\x1b[39m", fg)
	text = strings.ReplaceAll(text, "\x1b[49m", bg)
	return fg + bg + text + "\x1b[39m\x1b[49m"
}
func buttonStyle(text string, focused bool) string {
	return styledButton(text, focused, false)
}
func primaryButtonStyle(text string, focused bool) string {
	return styledButton(text, focused, true)
}
func styledButton(text string, focused, primary bool, focusColor ...string) string {
	text = strings.TrimPrefix(strings.TrimSuffix(ansi.Strip(text), "]"), "[")
	text = strings.TrimPrefix(text, "› ")
	if focused {
		text += " ↵"
	}
	text = "  " + text + "  "
	if !colorsEnabled() {
		return text
	}
	fg, bg := colorText, "\x1b[48;2;41;44;61m"
	if primary {
		fg, bg = "\x1b[38;2;0;0;0m", strings.Replace(colorAccent, "[38;", "[48;", 1)
	}
	if focused {
		fg, bg = "\x1b[38;2;0;0;0m", "\x1b[48;2;136;162;238m"
		if len(focusColor) > 0 {
			bg = strings.Replace(focusColor[0], "[38;", "[48;", 1)
		}
	}
	return filled(fg, bg, buttonLabelWeight(text))
}

func buttonLabelWeight(text string) string {
	if !colorsEnabled() {
		return text
	}
	return "\x1b[1m" + strings.ReplaceAll(text, "(space)", "\x1b[22m(space)\x1b[1m") + "\x1b[22m"
}

func badgeStyle(color, text string) string {
	text = " " + text + " "
	if !colorsEnabled() {
		return text
	}
	fg, bg := "\x1b[38;2;0;0;0m", strings.Replace(color, "[38;", "[48;", 1)
	if color == colorMuted {
		fg, bg = colorText, backgroundTab
	}
	return filled(fg, bg, "\x1b[1m"+text+"\x1b[22m")
}
func levelStyle(level precheck.Level, text string) string {
	switch level {
	case precheck.Ready:
		return badgeStyle(colorSuccess, text)
	case precheck.Blocked:
		return badgeStyle(colorError, text)
	default:
		return badgeStyle(colorWarning, text)
	}
}
func runtimeStyle(state, text string) string {
	switch strings.ToLower(state) {
	case "running":
		return ink(colorSuccess, text)
	case "stopped", "exited":
		return ink(colorMuted, text)
	case "pending", "starting", "stopping", "creating":
		return ink(colorWarning, text)
	case "failed", "error":
		return ink(colorError, text)
	default:
		return ink(colorInfo, text)
	}
}
func enabledStyle(enabled bool, text string) string {
	if enabled {
		return ink(colorSuccess, text)
	}
	return ink(colorMuted, text)
}
func statusStyle(state, text string) string {
	switch strings.ToLower(state) {
	case "ready", "connected", "registered":
		return ink(colorSuccess, text)
	case "failed", "blocked", "error", "manual intervention required":
		return ink(colorError, text)
	case "preparing", "installing", "pending", "entry ambiguous":
		return ink(colorWarning, text)
	default:
		return ink(colorMuted, text)
	}
}
func (m *model) selectedLine(l line) bool {
	for _, h := range l.hits {
		switch h.action {
		case "select":
			if m.page == "Sandboxes" && m.focus == "table" && m.input == nil && m.dialog == "" && h.id == m.selected && h.id != m.expanded {
				return true
			}
		case "plugin":
			if h.id == m.pluginSelected {
				return true
			}
		case "error":
			if m.focus == "errors" && h.id == strconv.Itoa(m.errorIndex) {
				return true
			}
		case "override":
			if m.focus == "plugins" && len(m.plugins) > 0 && h.id == m.plugins[min(m.detailIndex, len(m.plugins)-1)].ID {
				return true
			}
		}
	}
	return false
}
func (m *model) paintLine(l line) string {
	text := truncate(l.text, m.width)
	text += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(text)))
	if !colorsEnabled() {
		return text
	}
	if m.selectedLine(l) {
		for _, h := range l.hits {
			if h.action == "override" {
				selected := filled(foregroundSelected, backgroundSelected, ansi.Cut(text, h.x, h.x+h.w))
				return filled(colorText, background, ansi.Cut(text, 0, h.x)+selected+ansi.Cut(text, h.x+h.w, m.width))
			}
		}
		return filled(foregroundSelected, backgroundSelected, text)
	}
	return filled(colorText, background, text)
}

// outline uses the same cells for drawing and hit testing. It adds no focus or
// navigation state; long content still scrolls through the existing viewport.
func outline(lines []line, title string, width, indent int, borderColors ...string) []line {
	border := colorSeparator
	if len(borderColors) > 0 {
		border = borderColors[0]
	}
	if width-indent < 6 {
		return lines
	}
	inner := width - indent - 4
	label := truncate("─ "+title+" ", width-indent-2)
	top := strings.Repeat(" ", indent) + "╭" + label + strings.Repeat("─", max(0, width-indent-2-ansi.StringWidth(label))) + "╮"
	// Inline title colors must restore the border foreground after their reset.
	if colorsEnabled() {
		top = strings.ReplaceAll(top, "\x1b[39m", border)
	}
	result := []line{textLine(ink(border, top))}
	for _, l := range fitLines(lines, inner) {
		text := truncate(l.text, inner)
		content := strings.Repeat(" ", indent) + ink(border, "│") + " " + text + strings.Repeat(" ", max(0, inner-ansi.StringWidth(text))) + " " + ink(border, "│")
		hits := make([]hit, 0, len(l.hits))
		for _, h := range l.hits {
			h.w = min(h.w, max(0, inner-h.x))
			h.x += indent + 2
			if h.w > 0 {
				hits = append(hits, h)
			}
		}
		result = append(result, line{text: content, hits: hits})
	}
	result = append(result, textLine(ink(border, strings.Repeat(" ", indent)+"╰"+strings.Repeat("─", width-indent-2)+"╯")))
	return result
}

func stateDot(state string) string {
	color := colorWarning
	switch strings.ToLower(state) {
	case "running", "ready", "active":
		color = colorSuccess
	case "stopped", "exited":
		color = colorMuted
	case "failed", "blocked", "error":
		color = colorError
	}
	return ink(color, "●")
}
func stateLabel(state string) string {
	switch strings.ToLower(state) {
	case "running", "ready", "active":
		return "Running"
	case "starting", "pending":
		return "Starting"
	case "reconnecting":
		return "Reconnecting"
	case "stopped", "exited":
		return "Stopped"
	case "failed", "blocked", "error":
		return "Failed"
	default:
		return "Unknown"
	}
}

// managementCancelling reports work settling independently of the requested setting.
func (m *model) managementCancelling() bool {
	return m.worker.State == "cancelling" || !m.stopRequestedAt.IsZero()
}

func (m *model) managementStatus() string {
	state, color := "Error", colorError
	switch m.worker.State {
	case "running":
		state, color = "Running", colorSuccess
	case "starting", "pending", "reconnecting":
		state, color = "Pending", colorWarning
	case "stopped", "exited":
		state, color = "Stopped", colorMuted
	}
	text := ink(color, state)
	if m.managementCancelling() {
		text = ink(colorMuted, "Cancelling")
		if m.workerControlError {
			text += " " + ink(colorError, "ERROR") + " · " + ink(color, state)
		}
		return text
	}
	if m.workerControlError {
		return ink(colorError, "ERROR") + " · " + text
	}
	pending := m.refreshing || m.checksPending
	if m.worker.State == "running" && !m.worker.LastSync.IsZero() && time.Since(m.worker.LastSync) > 30*time.Second {
		stamp := "last at " + m.worker.LastSync.Local().Format("15:04")
		if pending {
			stamp += "*"
		}
		text += " " + ink(colorMuted, stamp)
	} else if pending {
		text += ink(colorWarning, " *")
	}
	return text
}
func (m *model) statusFooter() line {
	dockerState := "unknown"
	for _, check := range m.checks.Checks {
		if check.Name == "Sandbox daemon" {
			switch check.Level {
			case precheck.Ready:
				dockerState = "running"
			case precheck.Blocked:
				dockerState = "failed"
			}
		}
	}
	management := "Management " + enabledStyle(m.workerEnabled, strings.ToUpper(onOff(m.workerEnabled)))
	if m.managementCancelling() && m.width >= 42 {
		management += " " + ink(colorMuted, "Cancelling")
	}
	docker := "sbx " + stateDot(dockerState)
	if !colorsEnabled() {
		if m.width >= 60 {
			docker = "sbx: " + stateLabel(dockerState)
		} else {
			docker = "sbx " + runtimeDot(dockerState)
		}
	}
	gap := 2
	start := max(0, m.width-ansi.StringWidth(management)-gap-ansi.StringWidth(docker))
	return line{text: strings.Repeat(" ", start) + management + strings.Repeat(" ", gap) + docker, hits: []hit{{x: start, w: ansi.StringWidth(management), action: "worker"}, {x: start + ansi.StringWidth(management) + gap, w: ansi.StringWidth(docker), action: "docker"}}}
}
