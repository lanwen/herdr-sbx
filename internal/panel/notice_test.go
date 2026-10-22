package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestAssociationAndReadyGuidanceUseHint(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].AssociationIssue = "Two herdr entries match this sandbox."
	if hint, color := m.contextHint(); hint != m.rows[0].AssociationIssue || color != colorWarning {
		t.Fatal(hint, color)
	}
	for _, l := range m.sandboxDetails(m.rows[0]) {
		if strings.Contains(l.text, m.rows[0].AssociationIssue) {
			t.Fatal("guidance repeated in details")
		}
	}
	m.rows[0].AssociationIssue = ""
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.Detail = "already registered"
	if hint, color := m.contextHint(); hint != "This sandbox is registered in herdr. Setup is complete." || color != colorSuccess {
		t.Fatal(hint, color)
	}
	m.rows[0].Enabled = store.Disabled
	m.focus = "actions"
	if hint, color := m.contextHint(); !strings.HasPrefix(hint, "Enable this sandbox") || color != colorAccent {
		t.Fatal(hint, color)
	}
}

func TestHintWrapKeepsWordsWhole(t *testing.T) {
	for _, line := range wrapHint("Enable keeps the connection to herdr", 16) {
		if strings.Contains(line, "connec") && !strings.Contains(line, "connection") {
			t.Fatalf("hint split a word: %q", line)
		}
	}
	want := []string{"Enable keeps the", "connection to", "herdr"}
	got := wrapHint("Enable keeps the connection to herdr", 16)
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
	m.rows[1].Enabled = store.Enabled
	m.rows[1].EntryPresent, m.rows[1].ProfileID = true, "machine-b"
	m.rows[1].Status.Connection, m.rows[1].Status.Detail = "stopped", "Start beta to connect"
	m.key(keyMsg(tea.KeyDown, ""))
	if m.expanded != "b" || m.selected != "b" {
		t.Fatalf("navigation changed details: selected=%s expanded=%s", m.selected, m.expanded)
	}
	if hint, _ := m.contextHint(); hint != "Start beta to connect" {
		t.Fatalf("hint did not follow selection: %q", hint)
	}
	m.rows[1].Status.Detail = ""
	m.rows[1].Enabled = store.Disabled
	if hint, _ := m.contextHint(); hint != "" {
		t.Fatalf("empty saved detail produced hint: %q", hint)
	}
}

func TestStoppedMissingEntryPreservesSpecificLimitation(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].EntryPresent = false
	m.rows[0].Label = ""
	m.rows[0].Sandbox.Status = "stopped"
	m.rows[0].Status.Connection = "blocked"
	m.rows[0].Status.Detail = "This herdr version cannot add a stopped sandbox without connecting."
	if hint, color := m.contextHint(); hint != m.rows[0].Status.Detail || color != colorError {
		t.Fatal(hint, color)
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
	if len(lines) != m.height || !strings.Contains(lines[len(lines)-2], "? help") {
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
