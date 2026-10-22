package panel

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"time"
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
		if got != ansi.Strip(component.View(m.helpKeys())) || !strings.Contains(got, context.expect) {
			t.Fatalf("context %s: %q", context.expect, got)
		}
	}
	m.input = nil
	m.dialog = ""
	m.page, m.focus = "Sandboxes", "table"
	full := m.renderHelp(56, true)
	for _, key := range []string{"rename herdr label", "herdr plugin defaults", "status / Management", "fuzzy filter", "ctrl+c"} {
		if !strings.Contains(full, key) {
			t.Fatalf("full standard help omitted %q: %s", key, full)
		}
	}
}
func TestFooterIndicatorsStayAtBottomRightAndClickable(t *testing.T) {
	for _, width := range []int{60, 110} {
		for _, noColor := range []bool{false, true} {
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
			if len(lines) != 18 {
				t.Fatalf("height=%d", len(lines))
			}
			if strings.Contains(view, "herdr-sbx /") {
				t.Fatal("old title retained")
			}
			footer := lines[len(lines)-1]
			if !strings.HasSuffix(strings.TrimRight(footer, " "), func() string {
				if noColor {
					return "sbx: Running"
				}
				return "sbx ●"
			}()) {
				t.Fatalf("status not right aligned: %q", footer)
			}
			var daemon, docker hit
			for _, h := range m.hits {
				if h.action == "worker" {
					daemon = h
				}
				if h.action == "docker" {
					docker = h
				}
			}
			if daemon.x != docker.x-daemon.w-2 || daemon.x == 0 || daemon.y != 17 || docker.y != 17 || docker.x+docker.w != width || ansi.StringWidth(footer) != width || ansi.Cut(footer, daemon.x, daemon.x+daemon.w) != "Management ON" {
				t.Fatalf("footer bounds %#v %#v", daemon, docker)
			}
			m.click(daemon.x, daemon.y)
			if m.page != "Status" || m.dialog != "" {
				t.Fatal("daemon control does not open")
			}
			m.dialog = ""
			m.click(docker.x, docker.y)
			if m.page != "Status" {
				t.Fatal("sbx status does not open")
			}
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
		if found != want {
			t.Fatalf("Clear visibility=%v want %v", found, want)
		}
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
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "space disable") || !strings.Contains(got, "enter/→ details") || strings.Contains(got, "enable/disable") {
		t.Fatal(got)
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "rename" {
			m.button = i
		}
	}
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "enter rename") || !strings.Contains(got, "space disable") || !strings.Contains(got, "esc list") {
		t.Fatal(got)
	}
	m.selected, m.expanded, m.focus = "b", "b", "table"
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "space enable") {
		t.Fatal(got)
	}
	if got := ansi.Strip(m.renderHelp(110, true)); strings.Contains(got, "cycle tabs") {
		t.Fatal(got)
	}
}

func TestDetailsHelpExplainsHorizontalBackAtPrimaryAndPlugins(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnter, ""))
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "esc/← list") {
		t.Fatal(got)
	}
	m.key(keyMsg(tea.KeyRight, ""))
	if got := ansi.Strip(m.renderHelp(110, false)); strings.Contains(got, "esc/← list") || !strings.Contains(got, "esc list") {
		t.Fatal("Left is still action navigation:", got)
	}
	m.focus = "plugins"
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "esc/← list") {
		t.Fatal(got)
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
		if m.page != originalPage || m.focus != originalFocus || m.selected != originalSelection || m.expanded != originalExpansion || m.button != originalButton || m.scroll != originalScroll {
			t.Fatalf("help changed context: page=%s focus=%s selected=%s expanded=%s button=%d scroll=%d", m.page, m.focus, m.selected, m.expanded, m.button, m.scroll)
		}
	}
	m.key(keyMsg('?', "?"))
	if !m.helpOpen {
		t.Fatal("help did not open in place")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyPgDown, ""))
	assertContext()
	m.View()
	assertContext()
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.helpOpen {
		t.Fatal("Escape did not close help")
	}
	assertContext()
	m.key(keyMsg('?', "?"))
	m.key(keyMsg('?', "?"))
	if m.helpOpen {
		t.Fatal("question mark did not close help")
	}
	assertContext()
	m.key(keyMsg('?', "?"))
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.helpOpen || m.input == nil || m.input.kind != "rename" || m.input.target != originalSelection {
		t.Fatalf("selected action was not dispatched: help=%t input=%+v", m.helpOpen, m.input)
	}
}

func TestHelpOnlyAdvertisesEligibleActions(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = true
	m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
	available := func(want string) bool {
		for _, group := range m.helpKeys().full {
			for _, b := range group {
				for _, k := range b.Keys() {
					if k == want && b.Enabled() {
						return true
					}
				}
			}
		}
		return false
	}
	for _, k := range []string{"c", "d", "e", "n", "i"} {
		if available(k) {
			t.Fatalf("unavailable %s advertised", k)
		}
	}
	if !available("r") || !available("space") {
		t.Fatal("eligible sandbox actions omitted")
	}
	m.pending["a"] = true
	if available("r") || available("space") {
		t.Fatal("pending sandbox action advertised")
	}
	delete(m.pending, "a")
	m.workerEnabled = false
	if !available("c") {
		t.Fatal("useful refresh omitted")
	}
	m.refreshing = true
	if available("c") {
		t.Fatal("busy refresh advertised")
	}
	m.refreshing = false
	m.errors = []uiError{{code: store.CodeRename, scope: "a", dismissed: true, firstSeen: time.Now()}}
	if !available("e") {
		t.Fatal("dismissed diagnostic cannot be reached")
	}
	m.focus, m.showAllErrors = "errors", true
	if available("d") || !available("n") {
		t.Fatal("dismissed group dismissal advertised")
	}
	m.errors[0].dismissed = false
	m.errors[0].firstSeen = time.Time{}
	if available("d") {
		t.Fatal("unpersisted group dismissal advertised")
	}
	m.errors[0].firstSeen = time.Now()
	if !available("d") {
		t.Fatal("eligible dismissal omitted")
	}
	m.focus = "table"
	if available("n") {
		t.Fatal("diagnostic toggle advertised outside diagnostics")
	}
	m.plugins = []guestplugins.Choice{{ID: "unsupported", UnsupportedReason: "not transferable"}}
	m.page = "Plugins"
	if available("space") || available("r") {
		t.Fatal("ineligible plugin action advertised")
	}
	m.input = &inputState{kind: "rename"}
	for _, k := range []string{"?", "c", "e", "q", "r", "space", "tab"} {
		if available(k) {
			t.Fatalf("global %s advertised during input", k)
		}
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
		if !strings.Contains(colored, "\x1b[") || !strings.Contains(colored, "1;") {
			t.Fatalf("bold hotkeys missing: %q", colored)
		}
		if ansi.Strip(colored) != component.View(m.helpKeys()) {
			t.Fatal("standard compact help layout changed")
		}
		m.key(keyMsg('?', "?"))
		coloredFrame := m.View().Content
		for _, row := range strings.Split(ansi.Strip(coloredFrame), "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatalf("narrow help overflow: %q", row)
			}
		}
		t.Setenv("NO_COLOR", "")
		plain := m.View().Content
		if strings.Contains(plain, "\x1b[") {
			t.Fatal("NO_COLOR retained help styling")
		}
		coloredRows := strings.Split(ansi.Strip(coloredFrame), "\n")
		plainRows := strings.Split(plain, "\n")
		if strings.Join(coloredRows[:len(coloredRows)-1], "\n") != strings.Join(plainRows[:len(plainRows)-1], "\n") {
			t.Fatal("help colors changed geometry/content")
		}
	}
}

func TestHelpIgnoresUnadvertisedShortcutAndKeepsInputInterception(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = true
	m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
	m.key(keyMsg('?', "?"))
	if cmd := m.key(keyMsg('d', "d")); cmd != nil || !m.helpOpen {
		t.Fatal("unavailable shortcut dispatched")
	}
	m.key(keyMsg('/', "/"))
	if m.helpOpen || m.input == nil || m.input.kind != "filter" {
		t.Fatal("advertised slash did not open the filter")
	}
	m.input = nil
	m.beginRename()
	m.key(keyMsg('?', "?"))
	if m.helpOpen || !strings.HasSuffix(m.input.text, "?") {
		t.Fatal("help intercepted rename text")
	}
}

func TestHelpKeepsCompactCancellationAndOmitsMissingNextAction(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 30, 10
	m.worker.State = "cancelling"
	m.key(keyMsg('?', "?"))
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Cancelling") || len(strings.Split(frame, "\n")) != 10 {
		t.Fatalf("compact help lost cancellation: %s", frame)
	}
	m.helpOpen = false
	m.rows[0].EntryPresent = false
	m.rows[0].Sandbox.Status = "stopped"
	m.focus, m.button = "actions", 0
	for _, group := range m.helpKeys().full {
		for _, b := range group {
			for _, k := range b.Keys() {
				if k == "right" {
					t.Fatal("next action advertised for a single-action row")
				}
			}
		}
	}
}
