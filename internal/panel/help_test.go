package panel

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStandardHelpRendersContextAndFullBindings(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "example.plugin"}}
	for _, context := range []struct{ page, focus, input, dialog, expect string }{{"Sandboxes", "table", "", "", "details"}, {"Plugins", "table", "", "", "herdr plugin sync"}, {"Sandboxes", "plugins", "", "", "cycle choice"}, {"Sandboxes", "table", "rename", "", "save"}, {"Sandboxes", "table", "filter", "", "accept"}, {"Sandboxes", "table", "", "startup", "activate"}} {
		m.page, m.focus, m.dialog = context.page, context.focus, context.dialog
		m.input = nil
		if context.input != "" {
			m.input = &inputState{kind: context.input}
		}
		component := help.New()
		component.SetWidth(110)
		component.Styles = help.Styles{}
		got := m.renderHelp(110, false)
		require.False(t, got != ansi.Strip(component.View(m.helpKeys())))
		require.Contains(t, got, context.expect)
	}
	m.input = nil
	m.dialog = ""
	m.page, m.focus = "Sandboxes", "table"
	full := m.renderHelp(56, true)
	for _, key := range []string{"rename herdr label", "herdr plugin defaults", "status / Management", "fuzzy filter", "ctrl+c"} {
		require.Contains(t, full, key, "full standard help omitted %q: %s", key, full)
	}
}
func TestFooterIndicatorsStayAtBottomRightAndClickable(t *testing.T) {
	for _, width := range []int{60, 110} {
		for _, noColor := range []bool{false, true} {
			testFooterIndicators(t, width, noColor)
		}
	}
}
func TestFilterClearOnlyExistsForNonemptyQuery(t *testing.T) {
	m := modelFixture(t)
	check := func(want bool) {
		t.Helper()
		m.View()
		found := false
		for _, h := range m.hits {
			if h.action == "clear" {
				found = true
			}
		}
		require.Equal(t, want, found, "Clear visibility=%v want %v", found, want)
	}
	check(false)
	m.queries["Sandboxes"] = "alpha"
	check(true)
	m.input = &inputState{kind: "filter", text: ""}
	check(false)
	m.input.text = "a"
	m.input.cursor = 1
	check(false)
}

func TestSandboxHelpNamesPrimaryAndSelectedActions(t *testing.T) {
	m := modelFixture(t)
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.Contains(t, got, "space disable")
		require.Contains(t, got, "enter/→ details")
		require.NotContains(t, got, "enable/disable")
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "rename" {
			m.button = i
		}
	}
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.Contains(t, got, "enter rename")
		require.Contains(t, got, "space disable")
		require.Contains(t, got, "esc list")
	}
	m.selected, m.expanded, m.focus = "b", "b", "table"
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.Contains(t, got, "space enable", got)
	}
	{
		got := ansi.Strip(m.renderHelp(110, true))
		require.NotContains(t, got, "cycle tabs", got)
	}
}

func TestDetailsHelpExplainsHorizontalBackAtPrimaryAndPlugins(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnter, ""))
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.Contains(t, got, "esc/← list", got)
	}
	m.key(keyMsg(tea.KeyRight, ""))
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.NotContains(t, got, "esc/← list")
		require.Contains(t, got, "esc list")
	}
	m.focus = "plugins"
	{
		got := ansi.Strip(m.renderHelp(110, false))
		require.Contains(t, got, "esc/← list", got)
	}
}

func TestHelpPreservesContextWhileScrollingAndDispatchesSelectedAction(t *testing.T) {
	m := modelFixture(t)
	m.focus, m.scroll = "actions", 7
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "rename" {
			m.button = i
		}
	}
	originalPage, originalFocus, originalSelection, originalExpansion, originalButton, originalScroll := m.page, m.focus, m.selected, m.expanded, m.button, m.scroll
	assertContext := func() {
		t.Helper()
		require.False(t, m.page != originalPage)
		require.False(t, m.focus != originalFocus)
		require.False(t, m.selected != originalSelection)
		require.False(t, m.expanded != originalExpansion)
		require.False(t, m.button != originalButton)
		require.False(t, m.scroll != originalScroll)
	}
	m.key(keyMsg('?', "?"))
	require.True(t, m.helpOpen, "help did not open in place")
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyPgDown, ""))
	assertContext()
	m.View()
	assertContext()
	m.key(keyMsg(tea.KeyEscape, ""))
	require.False(t, m.helpOpen, "Escape did not close help")
	assertContext()
	m.key(keyMsg('?', "?"))
	m.key(keyMsg('?', "?"))
	require.False(t, m.helpOpen, "question mark did not close help")
	assertContext()
	m.key(keyMsg('?', "?"))
	m.key(keyMsg(tea.KeyEnter, ""))
	require.False(t, m.helpOpen)
	require.NotNil(t, m.input)
	require.EqualValues(t, "rename", m.input.kind)
	require.False(t, m.input.target != originalSelection)
}

func TestHelpOnlyAdvertisesEligibleActions(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = true
	m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
	available := func(want string) bool { return availableHelpKey(m, want) }

	for _, k := range []string{"c", "d", "e", "n", "i"} {
		require.False(t, available(k), "unavailable %s advertised", k)
	}
	require.True(t, available("r"))
	require.True(t, available("space"))
	m.pending["a"] = true
	require.False(t, available("r"))
	require.False(t, available("space"))
	delete(m.pending, "a")
	m.workerEnabled = false
	require.True(t, available("c"), "useful refresh omitted")
	m.refreshing = true
	require.False(t, available("c"), "busy refresh advertised")
	m.refreshing = false
	m.errors = []uiError{{code: store.CodeRename, scope: "a", dismissed: true, firstSeen: time.Now()}}
	require.True(t, available("e"), "dismissed diagnostic cannot be reached")
	m.focus, m.showAllErrors = "errors", true
	require.False(t, available("d"))
	require.True(t, available("n"))
	m.errors[0].dismissed = false
	m.errors[0].firstSeen = time.Time{}
	require.False(t, available("d"), "unpersisted group dismissal advertised")
	m.errors[0].firstSeen = time.Now()
	require.True(t, available("d"), "eligible dismissal omitted")
	m.focus = "table"
	require.False(t, available("n"), "diagnostic toggle advertised outside diagnostics")
	m.plugins = []guestplugins.Choice{{ID: "unsupported", UnsupportedReason: "not transferable"}}
	m.page = "Plugins"
	require.False(t, available("space"))
	require.False(t, available("r"))
	m.input = &inputState{kind: "rename"}
	for _, k := range []string{"?", "c", "e", "q", "r", "space", "tab"} {
		require.False(t, available(k), "global %s advertised during input", k)
	}
}

func TestHelpUsesStandardBoldKeysAndFitsColorlessNarrowViews(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		m := modelFixture(t)
		m.width, m.height = width, 10
		unsetNoColor(t)
		component := help.New()
		component.SetWidth(width)
		component.Styles = help.Styles{}
		colored := m.renderHelp(width, false)
		require.Contains(t, colored, "\x1b[")
		require.Contains(t, colored, "1;")
		require.Equal(t, component.View(m.helpKeys()), ansi.Strip(colored), "standard compact help layout changed")
		m.key(keyMsg('?', "?"))
		coloredFrame := m.View().Content
		for _, row := range strings.Split(ansi.Strip(coloredFrame), "\n") {
			require.False(t, ansi.StringWidth(row) > width, "narrow help overflow: %q", row)
		}
		t.Setenv("NO_COLOR", "")
		plain := m.View().Content
		require.NotContains(t, plain, "\x1b[", "NO_COLOR retained help styling")
		coloredRows := strings.Split(ansi.Strip(coloredFrame), "\n")
		plainRows := strings.Split(plain, "\n")
		require.Equal(t, strings.Join(plainRows[:len(plainRows)-1], "\n"), strings.Join(coloredRows[:len(coloredRows)-1], "\n"), "help colors changed geometry/content")
	}
}

func TestHelpIgnoresUnadvertisedShortcutAndKeepsInputInterception(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = true
	m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
	m.key(keyMsg('?', "?"))

	cmd := m.key(keyMsg('d', "d"))
	require.Nil(t, cmd)
	require.True(t, m.helpOpen)

	m.key(keyMsg('/', "/"))
	require.False(t, m.helpOpen)
	require.NotNil(t, m.input)
	require.EqualValues(t, "filter", m.input.kind)
	m.input = nil
	m.beginRename()
	m.key(keyMsg('?', "?"))
	require.False(t, m.helpOpen)
	require.True(t, strings.HasSuffix(m.input.text, "?"))
}

func TestHelpKeepsCompactCancellationAndOmitsMissingNextAction(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 30, 10
	m.worker.State = "cancelling"
	m.key(keyMsg('?', "?"))
	frame := ansi.Strip(m.View().Content)
	require.Contains(t, frame, "Cancelling")
	require.Len(t, strings.Split(frame, "\n"), 10)
	m.helpOpen = false
	m.rows[0].EntryPresent = false
	m.rows[0].Sandbox.Status = "stopped"
	m.focus, m.button = "actions", 0
	for _, group := range m.helpKeys().full {
		for _, b := range group {
			for _, k := range b.Keys() {
				require.NotEqual(t, "right", k, "next action advertised for a single-action row")
			}
		}
	}
}

func testFooterIndicators(t *testing.T, width int, noColor bool) {
	t.Helper()
	m := styledFixture(t)
	m.width, m.height = width, 18
	m.pending["worker"] = true
	m.workerEnabled = true
	unsetNoColor(t)
	if noColor {
		t.Setenv("NO_COLOR", "")
	}
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 18, "height=%d", len(lines))
	require.NotContains(t, view, "herdr-sbx /", "old title retained")
	footer := lines[len(lines)-1]
	require.True(t, strings.HasSuffix(strings.TrimRight(footer, " "), func() string {
		if noColor {
			return "sbx: Running"
		}
		return "sbx ●"
	}()), "status not right aligned: %q", footer)
	var daemon, docker hit
	for _, h := range m.hits {
		if h.action == "worker" {
			daemon = h
		}
		if h.action == "docker" {
			docker = h
		}
	}
	require.False(t, daemon.x != docker.x-daemon.w-2)
	require.NotEqualValues(t, 0, daemon.x)
	require.EqualValues(t, 17, daemon.y)
	require.EqualValues(t, 17, docker.y)
	require.False(t, docker.x+docker.w != width)
	require.False(t, ansi.StringWidth(footer) != width)
	require.EqualValues(t, "Management ON", ansi.Cut(footer, daemon.x, daemon.x+daemon.w))
	clickAt(m, daemon.x, daemon.y)
	require.EqualValues(t, "Status", m.page)
	require.EqualValues(t, "", m.dialog)
	docker = selectionHit(t, m, "docker", docker.id)
	clickAt(m, docker.x, docker.y)
	require.Equal(t, "Status", m.page, "sbx status does not open")

}

func availableHelpKey(m *model, want string) bool {
	for _, group := range m.helpKeys().full {
		for _, binding := range group {
			if slices.Contains(binding.Keys(), want) && binding.Enabled() {
				return true
			}
		}
	}
	return false
}
