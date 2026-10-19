package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestOwnershipConflictGuidanceUsesHint(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.expanded = "a"
	m.rows[0].Status.Connection = "ownership conflict"
	m.rows[0].Status.Detail = "a manual or untracked machine uses this label or target; choose a in the panel to adopt a matching machine"
	for _, line := range m.sandboxDetails(m.rows[0]) {
		if strings.Contains(line.text, "choose a in the panel") {
			t.Fatal("ownership guidance remained in details")
		}
	}
	view := m.View().Content
	if !strings.Contains(view, "╭─ Hint") || !strings.Contains(view, colorWarning) || !strings.Contains(ansi.Strip(view), "It was added outside the plugin") || strings.Contains(ansi.Strip(view), "choose a in the panel") {
		t.Fatalf("ownership guidance missing from colored Hint: %s", view)
	}
	m.rows[0].Status.Detail = "Another Herdr machine uses this name. Review it in Herdr-sbx."
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Another herdr machine uses this name. Review it in the plugin.") {
		t.Fatalf("ownership detail did not use hint wording: %s", view)
	}
	m.expanded = ""
	m.selected = "b"
	m.rows[1].Management = store.Watched
	if strings.Contains(ansi.Strip(m.View().Content), "It was added outside the plugin") {
		t.Fatal("ownership hint remained after selecting another sandbox")
	}
}

func TestAdoptionAndReadyGuidanceUseHint(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.expanded = "b"
	m.selected = "b"
	m.rows[1].Status.Connection = "ownership conflict"
	m.rows[1].Status.Detail = "This sandbox is already a Herdr machine. It was added outside Herdr-sbx."
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "It was added outside the plugin") || !strings.Contains(view, colorWarning) {
		t.Fatal("unmanaged context is missing from colored Hint")
	}
	m.focus, m.button = "actions", 0
	view = m.View().Content
	hint, color := m.contextHint()
	for _, want := range []string{"Adopt lets the plugin watch this sandbox", "sync selected herdr plugins", "name and connection stay the same", "remove the herdr machine when you delete the sandbox"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("Adopt explanation missing %q", want)
		}
	}
	if color != colorAccent || !strings.Contains(view, colorAccent) {
		t.Fatal("Adopt hint lost the primary action color")
	}
	m.dialog = "adopt"
	if !strings.Contains(ansi.Strip(m.View().Content), "Adopt lets the plugin watch this sandbox") {
		t.Fatal("Adopt explanation vanished from confirmation")
	}
	m.dialog = ""
	m.expanded, m.selected, m.focus = "a", "a", "table"
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.Detail = "already registered"
	for _, line := range m.sandboxDetails(m.rows[0]) {
		if strings.Contains(line.text, "already registered") {
			t.Fatal("ready guidance remained in details")
		}
	}
	view = m.View().Content
	if !strings.Contains(ansi.Strip(view), "This sandbox is registered in herdr.") || !strings.Contains(view, colorSuccess) {
		t.Fatal("ready guidance missing from colored Hint")
	}
	if strings.Contains(ansi.Strip(view), "reachable") || strings.Contains(ansi.Strip(view), "can connect") {
		t.Fatal("catalog registration claimed a fresh connection check")
	}
	for _, detail := range []string{"registered", "already registered and reachable", "registered and reachable"} {
		m.rows[0].Status.Detail = detail
		if hint, color := m.contextHint(); hint != "This sandbox is registered in herdr. Setup is complete." || color != colorSuccess {
			t.Fatalf("saved registration hint = %q / %q", hint, color)
		}
	}
}

func TestHintWrapKeepsWordsWhole(t *testing.T) {
	for _, line := range wrapHint("Adopt keeps the connection to herdr", 16) {
		if strings.Contains(line, "connec") && !strings.Contains(line, "connection") {
			t.Fatalf("hint split a word: %q", line)
		}
	}
	want := []string{"Adopt keeps the", "connection to", "herdr"}
	got := wrapHint("Adopt keeps the connection to herdr", 16)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrapped hint = %q, want %q", got, want)
	}
}

func TestSavedStatusDetailsFollowSelectedSandboxInHint(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "b"
	for _, tc := range []struct {
		state, detail, want, color string
	}{
		{"failed", "Herdr-sbx could not reach Herdr", "the plugin could not reach herdr", colorError},
		{"error", "Connection refused", "Connection refused", colorError},
		{"blocked", "SSH needs setup", "SSH needs setup", colorError},
		{"manual intervention required", "Repair the guest plugin", "Repair the guest plugin", colorError},
		{"stopped", "Sandbox is stopped", "Sandbox is stopped", colorMuted},
		{"pending", "Waiting for SSH", "Waiting for SSH", colorWarning},
		{"ready", "Connection is ready", "Connection is ready", colorSuccess},
	} {
		t.Run(tc.state, func(t *testing.T) {
			m.rows[0].Status.Connection, m.rows[0].Status.Detail = tc.state, tc.detail
			got, color := m.contextHint()
			if got != tc.want || color != tc.color {
				t.Fatalf("hint = %q / %q, want %q / %q", got, color, tc.want, tc.color)
			}
		})
	}
	m.rows[1].Management = store.Watched
	m.rows[1].Owned, m.rows[1].ProfileID = true, "machine-b"
	m.rows[1].Status.Connection, m.rows[1].Status.Detail = "stopped", "Start beta to connect"
	m.key(keyMsg(tea.KeyDown, ""))
	if m.expanded != "b" || m.selected != "b" {
		t.Fatalf("navigation changed details: selected=%s expanded=%s", m.selected, m.expanded)
	}
	if hint, _ := m.contextHint(); hint != "Start beta to connect" {
		t.Fatalf("hint did not follow selection: %q", hint)
	}
	m.rows[1].Status.Detail = ""
	if hint, _ := m.contextHint(); hint != "" {
		t.Fatalf("empty saved detail produced hint: %q", hint)
	}
}

func TestStoppedWatchedSandboxWithoutRegistrationExplainsDeferredRegistration(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].Management = store.Watched
	m.rows[0].Owned = false
	m.rows[0].ProfileID = ""
	m.rows[0].Sandbox.Status = "stopped"
	m.rows[0].Status.Detail = "sandbox deletion recovery is unavailable"
	hint, color := m.contextHint()
	if hint != "This sandbox is stopped. The plugin will try to add its herdr machine when it starts." || color != colorMuted {
		t.Fatalf("stopped watched hint = %q / %q", hint, color)
	}
}

func TestProductNamesInNoticesAndEvents(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.notice = "Herdr-sbx is waiting for Herdr"
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "the plugin is waiting for herdr") {
		t.Fatalf("notice did not use hint wording: %s", view)
	}
	m.events = []store.EventRecord{{Detail: "Herdr-sbx could not reach Herdr"}}
	var rendered string
	for _, line := range m.eventLines() {
		rendered += ansi.Strip(line.text)
	}
	if !strings.Contains(rendered, "herdr-sbx could not reach herdr") {
		t.Fatalf("event retained product capitals: %s", rendered)
	}
}

func TestErrorSectionFocusKeepsTabAndFooter(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height = 60, 18
	content := m.View().Content
	if strings.Contains(content, "Errors ·") || strings.Contains(content, "Events ·") || strings.Contains(content, "Hint") {
		t.Fatalf("empty section rendered: %s", content)
	}
	m.addError(store.CodeSSHSetup, "Host", errNotice("missing configuration"))
	m.key(keyMsg('e', "e"))
	content = m.View().Content
	for _, want := range []string{"╭─ Errors · New (1) | All (1)", store.CodeSSHSetup + " · Host", "missing configuration"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in %s", want, content)
		}
	}
	if m.page != "Sandboxes" || m.focus != "errors" {
		t.Fatalf("error focus changed tab: %s/%s", m.page, m.focus)
	}
	lines := strings.Split(content, "\n")
	if len(lines) != m.height || !strings.Contains(lines[len(lines)-1], "? help") {
		t.Fatalf("footer displaced: %s", content)
	}
	for _, h := range m.hits {
		if h.action == "error" {
			m.click(h.x, h.y)
			if m.focus != "errors" || m.page != "Sandboxes" {
				t.Fatal("first host error click did not keep diagnostics selected")
			}
			m.click(h.x, h.y)
			if m.page != "Status" {
				t.Fatal("host error click did not open status recovery")
			}
			return
		}
	}
	t.Fatal("error recovery not clickable")
}

func TestBlockedInputHintNamesEditAndAttemptedControl(t *testing.T) {
	m := modelFixture(t)
	m.input = &inputState{kind: "rename", target: "a", text: "private new label"}
	m.hitAction(hit{action: "page", id: "Plugins"})
	if m.page != "Sandboxes" || !strings.Contains(m.notice, "Renaming herdr label for alpha") ||
		!strings.Contains(m.notice, "before opening Plugins") || strings.Contains(m.notice, "private new label") {
		t.Fatalf("rename hint does not identify blocked action safely: %q", m.notice)
	}
	m.input = &inputState{kind: "filter", text: "secret filter"}
	m.hitAction(hit{action: "refresh"})
	if !strings.Contains(m.notice, "Filtering Sandboxes") || !strings.Contains(m.notice, "before refreshing observations") ||
		strings.Contains(m.notice, "secret filter") {
		t.Fatalf("filter hint does not identify blocked action safely: %q", m.notice)
	}
	m.rows[0].Sandbox.Name = "alpha\x1b[31m\nline"
	m.hitAction(hit{action: "select", id: "a"})
	if strings.ContainsAny(m.notice, "\x1b\n") || !strings.Contains(m.notice, "alpha[31m line") {
		t.Fatalf("sandbox name introduced terminal controls: %q", m.notice)
	}
	m.hitAction(hit{action: "plugin", id: "plugin\x1b[31m"})
	if strings.Contains(m.notice, "\x1b") || !strings.Contains(m.notice, "plugin[31m") {
		t.Fatalf("plugin ID introduced terminal controls: %q", m.notice)
	}
}

func TestEventHistoryCanBeReadAfterActiveErrors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.addError(store.CodeGuestPluginSync, "guest.plugin", errNotice("dependency missing"))
	for i := 0; i < 12; i++ {
		m.events = append(m.events, store.EventRecord{Code: store.CodeGuestPluginSync, Scope: "guest.plugin", Detail: "Diagnostic event " + string(rune('a'+i)), Resolved: i%2 == 0, At: time.Date(2026, 10, 15, 9, i, 0, 0, time.Local)})
	}
	m.selected = "b"
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "events" || m.eventsExpanded {
		t.Fatal("Events did not start collapsed and keyboard accessible")
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEnd, ""))
	m.View()
	m.key(keyMsg(tea.KeyPgDown, ""))
	content := m.View().Content
	if !strings.Contains(content, "Diagnostic event a") {
		t.Fatalf("oldest event not reachable: %s", content)
	}
	m.key(keyMsg('e', "e"))
	content = m.View().Content
	if !strings.Contains(content, store.CodeGuestPluginSync+" · guest.plugin") {
		t.Fatalf("active error not reachable: %s", content)
	}

}

func TestGuestPluginErrorOpensAffectedSandbox(t *testing.T) {
	m := modelFixture(t)
	m.addError(store.CodeGuestPluginResponse, store.StatusKey("scope", "a"), errNotice("unexpected guest plugin response: expected 7 fields, got 8"))
	m.key(keyMsg('e', "e"))
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.page != "Sandboxes" || m.selected != "a" || m.expanded != "a" || m.focus != "actions" || m.sandboxActions(m.rows[0])[m.button].id != "resync" {
		t.Fatalf("guest plugin recovery target: page=%s selected=%s expanded=%s focus=%s", m.page, m.selected, m.expanded, m.focus)
	}
}

type errNotice string

func (e errNotice) Error() string { return string(e) }
