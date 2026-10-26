package panel

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func unsetNoColor(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))
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
				testThemeGeometry(t, size.width, size.height, page)
			})
		}
	}
}
func TestThemeUsesSemanticColorsAndFocusedButtonFill(t *testing.T) {
	unsetNoColor(t)
	m := styledFixture(t)
	m.workerEnabled = true
	view := m.View().Content
	for _, color := range []string{colorSuccess, colorSand, colorError, colorSeparator} {
		require.Contains(t, view, color, "semantic color %q missing", color)
	}
	m.dialog = "startup"
	m.button = 1
	view = m.View().Content
	require.Contains(t, view, "\x1b[48;2;136;162;238m\x1b[1m  Start Management ↵  ", "focused button has no selected fill")
	t.Setenv("NO_COLOR", "")
	plain := m.View().Content
	require.Contains(t, plain, "  Start Management ↵  ")
	require.Contains(t, plain, "Management ON")
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
			require.True(t, strings.HasPrefix(afterTitle, colorSeparator))
			require.Contains(t, afterTitle, "─╮")
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
			{
				title := m.diagnosticTitle()
				require.Contains(t, title, ink(color, scenario.title), "focus %s: incorrect header color: %q", focus, title)
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
		require.Contains(t, view, state.color+m.eventTitle()+colorSeparator, "focus %s index %d: incorrect Events header color: %q", state.focus, state.index, view)
	}
}

func TestSandboxRowFillFollowsTableFocus(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.expanded = ""
	var row line
	for _, l := range m.sandboxLines() {
		for _, h := range l.hits {
			if h.action == "select" && h.id == m.selected {
				row = l
			}
		}
	}
	require.NotEqual(t, 0, len(row.hits), "selected sandbox row missing")
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

		got := strings.Contains(m.paintLine(row), backgroundSelected)
		require.Equal(t, state.want, got, "state %+v: row fill=%v", state, got)

		require.Equal(t, selected, m.selected, "moving focus changed sandbox selection")
	}
}

func TestEmptyDiagnosticsHavePaddedReadableMessage(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeRename, dismissed: true}}
	for _, width := range []int{60, 110} {
		m.width = width
		lines := m.errorLines()
		require.False(t, len(lines) < 3)
		require.EqualValues(t, "", lines[0].text)
		require.EqualValues(t, "", lines[len(lines)-1].text)
		var words []string
		for _, l := range lines[1 : len(lines)-1] {
			require.True(t, strings.HasPrefix(ansi.Strip(l.text), "  "))
			require.False(t, ansi.StringWidth(l.text) > width-4)
			words = append(words, strings.Fields(ansi.Strip(l.text))...)
		}

		got := strings.Join(words, " ")
		require.Equal(t, "No new diagnostics. Switch to All to see dismissed active entries.", got, "width %d: message changed while wrapping: %q", width, got)
	}
}
func TestOutlinedLongInputsKeepCursorVisible(t *testing.T) {
	for _, width := range []int{60, 80, 110} {
		for _, kind := range []string{"rename", "filter"} {
			t.Run(fmt.Sprintf("%s-%d", kind, width), func(t *testing.T) {
				testOutlinedInput(t, width, kind)
			})
		}
	}
}

func TestFilterPromptFocusAndActiveQueryColors(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 80
	line := m.filterLine(2, 2, "sandboxes")
	require.Contains(t, line.text, colorMuted+"/ Filter…")
	require.EqualValues(t, "/ Filter…   2 of 2 sandboxes", ansi.Strip(line.text))
	require.Len(t, line.hits, 1)
	require.EqualValues(t, "filter", line.hits[0].action)
	m.queries["Sandboxes"] = "alpha"
	line = m.filterLine(2, 1, "sandboxes")
	require.NotContains(t, line.text, colorMuted+"/ alpha")
	require.Contains(t, ansi.Strip(line.text), "/ alpha   1 of 2 sandboxes")
	require.Len(t, line.hits, 2)
	require.EqualValues(t, "clear", line.hits[1].action)
	m.input = &inputState{kind: "filter", text: "alpha", cursor: 5}
	line = m.filterLine(2, 1, "sandboxes")
	require.Contains(t, line.text, colorAccent+"/ ")
	require.Contains(t, line.text, colorBright+"alpha│")
	require.NotContains(t, ansi.Strip(line.text), "Clear")
	require.Len(t, line.hits, 1)
}

func testThemeGeometry(t *testing.T, width, height int, page string) {
	t.Helper()
	m := styledFixture(t)
	m.width, m.height = width, height
	m.page = page
	unsetNoColor(t)
	colored := m.View().Content
	hits := append([]hit(nil), m.hits...)
	require.Contains(t, colored, colorSuccess)
	require.Contains(t, colored, background)
	require.Contains(t, ansi.Strip(colored), "╭")
	require.Contains(t, ansi.Strip(colored), "│")
	for _, value := range []string{"", "1"} {
		t.Setenv("NO_COLOR", value)
		plain := m.View().Content
		require.NotContains(t, plain, "\x1b[", "NO_COLOR=%q retained styling", value)
		coloredLines := strings.Split(ansi.Strip(colored), "\n")
		plainLines := strings.Split(plain, "\n")
		require.Equal(t, strings.Join(plainLines[:len(plainLines)-1], "\n"), strings.Join(coloredLines[:len(coloredLines)-1], "\n"), "color changed layout/content at %dx%d", width, height)
		require.Equal(t, m.hits[:len(m.hits)-2], hits[:len(hits)-2], "color changed mouse geometry")
	}
	for _, line := range strings.Split(ansi.Strip(colored), "\n") {
		require.Equal(t, width, ansi.StringWidth(line), "line width=%d, want %d", ansi.StringWidth(line), width)
	}

}

func testOutlinedInput(t *testing.T, width int, kind string) {
	t.Helper()
	unsetNoColor(t)
	m := styledFixture(t)
	m.width, m.height = width, 18
	m.input = &inputState{kind: kind, target: "a", text: strings.Repeat("long ", 40), cursor: 200}
	view := ansi.Strip(m.View().Content)
	caret := "│"
	if kind == "rename" {
		caret = "█"
	}
	require.Contains(t, view, "long "+caret, "cursor clipped: %s", view)
	if kind == "filter" {
		require.NotContains(t, view, "Clear", "filter input displayed Clear while editing: %s", view)
	}

}
