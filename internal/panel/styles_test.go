package panel

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func unsetNoColor(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
}
func styledFixture(t *testing.T) *model {
	m := modelFixture(t)
	m.worker = store.WorkerStatus{State: "running"}
	m.plugins = []guestplugins.Choice{{ID: "review.plugin", Enabled: true, Installed: true}, {ID: "tools.plugin"}}
	m.pluginSelected = "review.plugin"
	m.checks.Checks = []precheck.Check{{Name: "Sandbox daemon", Level: precheck.Ready, Detail: "Available"}, {Name: "Sandbox SSH", Level: precheck.Blocked, Detail: "Configuration missing"}}
	m.errors = []uiError{{code: store.CodeSSHSetup, scope: "Host", detail: "Configuration missing", count: 1}}
	m.rows[0].Status.Connection = "ready"
	return m
}

func TestThemeRetainsContentGeometryAndMouseBoundsWithoutColor(t *testing.T) {
	for _, size := range []struct{ width, height int }{{110, 32}, {60, 18}} {
		for _, page := range []string{"Sandboxes", "Plugins", "Status"} {
			t.Run(fmt.Sprintf("%s-%d", page, size.width), func(t *testing.T) {
				m := styledFixture(t)
				m.width, m.height = size.width, size.height
				m.page = page
				unsetNoColor(t)
				colored := m.View().Content
				hits := append([]hit(nil), m.hits...)
				if !strings.Contains(colored, colorSuccess) || !strings.Contains(colored, background) {
					t.Fatal("theme missing from colored view")
				}
				if !strings.Contains(ansi.Strip(colored), "╭") || !strings.Contains(ansi.Strip(colored), "│") {
					t.Fatal("section outline missing")
				}
				for _, value := range []string{"", "1"} {
					t.Setenv("NO_COLOR", value)
					plain := m.View().Content
					if strings.Contains(plain, "\x1b[") {
						t.Fatalf("NO_COLOR=%q retained styling", value)
					}
					coloredLines := strings.Split(ansi.Strip(colored), "\n")
					plainLines := strings.Split(plain, "\n")
					if strings.Join(coloredLines[:len(coloredLines)-1], "\n") != strings.Join(plainLines[:len(plainLines)-1], "\n") {
						t.Fatalf("color changed layout/content at %dx%d", size.width, size.height)
					}
					if !reflect.DeepEqual(hits[:len(hits)-2], m.hits[:len(m.hits)-2]) {
						t.Fatal("color changed mouse geometry")
					}
				}
				for _, line := range strings.Split(ansi.Strip(colored), "\n") {
					if ansi.StringWidth(line) != size.width {
						t.Fatalf("line width=%d, want %d", ansi.StringWidth(line), size.width)
					}
				}
			})
		}
	}
}
func TestThemeUsesSemanticColorsAndFocusedButtonFill(t *testing.T) {
	unsetNoColor(t)
	m := styledFixture(t)
	view := m.View().Content
	for _, color := range []string{colorSuccess, colorSand, colorWarning, colorError, backgroundSelected, colorSeparator} {
		if !strings.Contains(view, color) {
			t.Fatalf("semantic color %q missing", color)
		}
	}
	m.dialog = "startup"
	m.button = 1
	view = m.View().Content
	if !strings.Contains(view, "\x1b[48;2;136;162;238m\x1b[1m  Start auto-sync ↵  ") {
		t.Fatal("focused button has no selected fill")
	}
	t.Setenv("NO_COLOR", "")
	plain := m.View().Content
	if !strings.Contains(plain, "  Start auto-sync ↵  ") || !strings.Contains(plain, "Auto-sync ON") {
		t.Fatal("colorless controls lost labels")
	}
}

func TestOutlineRestoresBorderColorAfterStyledTitle(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeRename}}
	for _, focused := range []bool{false, true} {
		m.focus = "table"
		if focused {
			m.focus = "diagnostics"
		}
		for _, all := range []bool{false, true} {
			m.showAllErrors = all
			top := outline(nil, m.diagnosticTitle(), 110, 0)[0].text
			afterTitle := top[strings.LastIndex(top, "All (1)")+len("All (1)"):]
			afterTitle = strings.TrimPrefix(afterTitle, "\x1b[22m")
			if !strings.HasPrefix(afterTitle, colorSeparator) || !strings.Contains(afterTitle, "─╮") {
				t.Fatalf("title leaked foreground into border: %q", top)
			}
		}
	}
}

func TestDiagnosticAndEventsHeadersShowActionableFocus(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	for _, scenario := range []struct {
		title  string
		errors []uiError
	}{
		{"Errors", []uiError{{code: store.CodeRename}}},
		{"Warnings", []uiError{{code: store.CodeOwnershipConflict}}},
		{"Errors and warnings", []uiError{{code: store.CodeRename}, {code: store.CodeOwnershipConflict}}},
	} {
		m.errors = scenario.errors
		for _, focus := range []string{"table", "diagnostics", "errors", "events"} {
			m.focus = focus
			color := colorSeparator
			if focus == "diagnostics" {
				color = colorText
			}
			if title := m.diagnosticTitle(); !strings.Contains(title, ink(color, scenario.title)) {
				t.Fatalf("focus %s: incorrect header color: %q", focus, title)
			}
		}
	}
	m.events = []store.EventRecord{{Code: store.CodeRename, Detail: "Rename failed"}}
	for _, state := range []struct {
		focus string
		index int
		color string
	}{{"table", -1, colorSeparator}, {"events", -1, colorText}, {"events", 0, colorSeparator}, {"diagnostics", -1, colorSeparator}} {
		m.focus, m.eventIndex = state.focus, state.index
		view := m.View().Content
		if !strings.Contains(view, state.color+m.eventTitle()+colorSeparator) {
			t.Fatalf("focus %s index %d: incorrect Events header color: %q", state.focus, state.index, view)
		}
	}
}

func TestSandboxRowFillFollowsTableFocus(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	var row line
	for _, l := range m.sandboxLines() {
		for _, h := range l.hits {
			if h.action == "select" && h.id == m.selected {
				row = l
			}
		}
	}
	if len(row.hits) == 0 {
		t.Fatal("selected sandbox row missing")
	}
	selected := m.selected
	for _, state := range []struct {
		page, focus, dialog, input string
		want                       bool
	}{
		{"Sandboxes", "table", "", "", true},
		{"Sandboxes", "plugins", "", "", false},
		{"Sandboxes", "actions", "", "", false},
		{"Sandboxes", "events", "", "", false},
		{"Sandboxes", "diagnostics", "", "", false},
		{"Sandboxes", "errors", "", "", false},
		{"Sandboxes", "worker", "", "", false},
		{"Plugins", "table", "", "", false},
		{"Status", "table", "", "", false},
		{"Sandboxes", "table", "startup", "", false},
		{"Sandboxes", "table", "", "filter", false},
		{"Sandboxes", "table", "", "rename", false},
		{"Sandboxes", "table", "", "", true},
	} {
		m.page, m.focus, m.dialog, m.input = state.page, state.focus, state.dialog, nil
		if state.input != "" {
			m.input = &inputState{kind: state.input}
		}
		if got := strings.Contains(m.paintLine(row), backgroundSelected); got != state.want {
			t.Fatalf("state %+v: row fill=%v", state, got)
		}
		if m.selected != selected {
			t.Fatal("moving focus changed sandbox selection")
		}
	}
}

func TestEmptyDiagnosticsHavePaddedReadableMessage(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeRename, dismissed: true}}
	for _, width := range []int{60, 110} {
		m.width = width
		lines := m.errorLines()
		if len(lines) < 3 || lines[0].text != "" || lines[len(lines)-1].text != "" {
			t.Fatalf("width %d: empty diagnostics lack vertical padding: %+v", width, lines)
		}
		var words []string
		for _, l := range lines[1 : len(lines)-1] {
			if !strings.HasPrefix(ansi.Strip(l.text), "  ") || ansi.StringWidth(l.text) > width-4 {
				t.Fatalf("width %d: message lacks inset or exceeds outline: %q", width, l.text)
			}
			words = append(words, strings.Fields(ansi.Strip(l.text))...)
		}
		if got := strings.Join(words, " "); got != "No new diagnostics. Switch to All to see dismissed active entries." {
			t.Fatalf("width %d: message changed while wrapping: %q", width, got)
		}
	}
}
func TestOutlinedMouseControlsUseRenderedCells(t *testing.T) {
	unsetNoColor(t)
	m := styledFixture(t)
	m.View()
	var expand hit
	for _, h := range m.hits {
		if h.action == "expand" && h.id == "a" {
			expand = h
		}
	}
	row := strings.Split(ansi.Strip(m.View().Content), "\n")[expand.y]
	if ansi.Cut(row, expand.x, expand.x+expand.w) != "▸ " {
		t.Fatalf("disclosure hit does not match render: %#v %q", expand, row)
	}
	m.click(expand.x, expand.y)
	if m.expanded != "a" {
		t.Fatal("outlined disclosure cannot activate")
	}
}

func TestOutlinedLongInputsKeepCursorVisible(t *testing.T) {
	for _, width := range []int{60, 80, 110} {
		for _, kind := range []string{"rename", "filter"} {
			t.Run(fmt.Sprintf("%s-%d", kind, width), func(t *testing.T) {
				unsetNoColor(t)
				m := styledFixture(t)
				m.width, m.height = width, 18
				m.input = &inputState{kind: kind, target: "a", text: strings.Repeat("long ", 40), cursor: 200}
				view := ansi.Strip(m.View().Content)
				if !strings.Contains(view, "long │") {
					t.Fatalf("cursor clipped: %s", view)
				}
				if kind == "filter" {
					if strings.Contains(view, "Clear") {
						t.Fatalf("filter input displayed Clear while editing: %s", view)
					}
				}
			})
		}
	}
}

func TestFilterPromptFocusAndActiveQueryColors(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 80
	line := m.filterLine(2, 2, "sandboxes")
	if !strings.Contains(line.text, colorMuted+"/ Filter…") || ansi.Strip(line.text) != "/ Filter…   2 of 2 sandboxes" {
		t.Fatalf("idle filter prompt should be muted: %q", line.text)
	}
	if len(line.hits) != 1 || line.hits[0].action != "filter" {
		t.Fatalf("idle filter hit changed: %+v", line.hits)
	}
	m.queries["Sandboxes"] = "alpha"
	line = m.filterLine(2, 1, "sandboxes")
	if strings.Contains(line.text, colorMuted+"/ alpha") || !strings.Contains(ansi.Strip(line.text), "/ alpha   1 of 2 sandboxes") {
		t.Fatalf("active query should stay readable: %q", line.text)
	}
	if len(line.hits) != 2 || line.hits[1].action != "clear" {
		t.Fatalf("active query Clear hit changed: %+v", line.hits)
	}
	m.input = &inputState{kind: "filter", text: "alpha", cursor: 5}
	line = m.filterLine(2, 1, "sandboxes")
	if !strings.Contains(line.text, colorAccent+"/ ") || !strings.Contains(line.text, colorBright+"alpha│") ||
		strings.Contains(ansi.Strip(line.text), "Clear") || len(line.hits) != 1 {
		t.Fatalf("focused input lost contrast or displayed Clear: %q", line.text)
	}
}
