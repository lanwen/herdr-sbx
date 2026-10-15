package panel

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/help"
	"github.com/charmbracelet/x/ansi"
)

func TestStandardHelpRendersContextAndFullBindings(t *testing.T) {
	m := modelFixture(t)
	for _, context := range []struct{ page, focus, input, dialog, expect string }{{"Sandboxes", "table", "", "", "details"}, {"Plugins", "table", "", "", "auto-sync"}, {"Sandboxes", "plugins", "", "", "cycle choice"}, {"Sandboxes", "table", "rename", "", "save"}, {"Sandboxes", "table", "filter", "", "accept"}, {"Sandboxes", "table", "", "startup", "activate"}} {
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
	full := m.renderHelp(56, true)
	for _, key := range []string{"watch/unwatch", "plugin defaults", "status / auto-sync", "fuzzy filter", "diagnostics", "ctrl+c"} {
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
			if daemon.y != 17 || docker.y != 17 || docker.x+docker.w != width || ansi.StringWidth(footer) != width || ansi.Cut(footer, daemon.x, daemon.x+daemon.w) != "Auto-sync ON*" {
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

func TestAutoSyncFooterHasColoredTriStateLabel(t *testing.T) {
	for _, test := range []struct{ state, label, color string }{{"running", "ON", colorSuccess}, {"starting", "OFF", colorMuted}, {"reconnecting", "ERROR", colorError}, {"unknown", "ERROR", colorError}, {"stopped", "OFF", colorMuted}, {"failed", "ERROR", colorError}} {
		unsetNoColor(t)
		m := modelFixture(t)
		m.worker.State = test.state
		if got := m.statusFooter().text; !strings.Contains(got, test.color+test.label) || !strings.Contains(ansi.Strip(got), "Auto-sync "+test.label) || strings.Contains(ansi.Strip(got), "Auto-sync ●") {
			t.Fatalf("state %s: %q", test.state, got)
		}
		t.Setenv("NO_COLOR", "")
		footer := m.statusFooter().text
		if !strings.Contains(footer, "Auto-sync "+test.label) || strings.Contains(footer, "●") || strings.Contains(footer, "\x1b[") {
			t.Fatal(footer)
		}
	}
}

func TestAutoSyncFooterMarksOnlyDaemonTransitions(t *testing.T) {
	for _, test := range []struct {
		name, state, label string
		command, stopping  bool
		failed, observing  bool
	}{
		{name: "start command", state: "stopped", label: "OFF*", command: true},
		{name: "stop command", state: "running", label: "ON*", command: true, stopping: true},
		{name: "stop awaiting exit", state: "running", label: "ON*", stopping: true},
		{name: "starting", state: "starting", label: "OFF*"},
		{name: "pending", state: "pending", label: "ERROR*"},
		{name: "reconnecting", state: "reconnecting", label: "ERROR*"},
		{name: "running", state: "running", label: "ON"},
		{name: "stopped", state: "stopped", label: "OFF"},
		{name: "exit observed", state: "stopped", label: "OFF", stopping: true},
		{name: "error", state: "error", label: "ERROR"},
		{name: "stop failed", state: "running", label: "ON", stopping: true, failed: true},
		{name: "start failed", state: "stopped", label: "OFF", failed: true},
		{name: "observation refresh", state: "running", label: "ON", observing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			unsetNoColor(t)
			m := modelFixture(t)
			m.worker.State = test.state
			m.pending["worker"] = test.command
			m.workerControlError = test.failed
			m.refreshing, m.checksPending = test.observing, test.observing
			if test.stopping {
				m.stopRequestedAt = time.Now()
			}
			_, color := autoSyncIndicator(test.state)
			footer := m.statusFooter()
			if !strings.Contains(footer.text, color+test.label+"\x1b[39m") || ansi.Strip(ansi.Cut(footer.text, footer.hits[0].x, footer.hits[0].x+footer.hits[0].w)) != "Auto-sync "+test.label {
				t.Fatalf("unexpected footer: %q", footer.text)
			}
			t.Setenv("NO_COLOR", "")
			footer = m.statusFooter()
			if strings.Contains(footer.text, "\x1b[") || ansi.Cut(footer.text, footer.hits[0].x, footer.hits[0].x+footer.hits[0].w) != "Auto-sync "+test.label {
				t.Fatalf("unexpected plain footer: %q", footer.text)
			}
		})
	}
}
