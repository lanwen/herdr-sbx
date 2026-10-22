package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestAssociationAndReadyGuidanceUseHint(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].AssociationIssue = "Two herdr entries match this sandbox."
	{
		hint, color := m.contextHint()
		require.False(t, hint != m.rows[0].AssociationIssue)
		require.False(t, color != colorWarning)
	}
	for _, l := range m.sandboxDetails(m.rows[0]) {
		require.NotContains(t, l.text, m.rows[0].AssociationIssue, "guidance repeated in details")
	}
	m.rows[0].AssociationIssue = ""
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.Detail = "already registered"
	{
		hint, color := m.contextHint()
		require.EqualValues(t, "This sandbox is registered in herdr. Setup is complete.", hint)
		require.False(t, color != colorSuccess)
	}
	m.rows[0].Enabled = store.Disabled
	m.focus = "actions"
	{
		hint, color := m.contextHint()
		require.True(t, strings.HasPrefix(hint, "Enable this sandbox"))
		require.False(t, color != colorAccent)
	}
}

func TestHintWrapKeepsWordsWhole(t *testing.T) {
	for _, line := range wrapHint("Enable keeps the connection to herdr", 16) {
		require.False(t, strings.Contains(line, "connec") && !strings.Contains(line, "connection"), "hint split a word: %q", line)
	}
	want := []string{"Enable keeps the", "connection to", "herdr"}
	got := wrapHint("Enable keeps the connection to herdr", 16)
	require.Equal(t, strings.Join(want, "|"), strings.Join(got, "|"), "wrapped hint = %q, want %q", got, want)
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
			require.False(t, got != tc.want)
			require.False(t, color != tc.color)
		})
	}
	m.rows[1].Enabled = store.Enabled
	m.rows[1].EntryPresent, m.rows[1].ProfileID = true, "machine-b"
	m.rows[1].Status.Connection, m.rows[1].Status.Detail = "stopped", "Start beta to connect"
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "b", m.selected)
	{
		hint, _ := m.contextHint()
		require.Equal(t, "Start beta to connect", hint, "hint did not follow selection: %q", hint)
	}
	m.rows[1].Status.Detail = ""
	m.rows[1].Enabled = store.Disabled
	{
		hint, _ := m.contextHint()
		require.Equal(t, "", hint, "empty saved detail produced hint: %q", hint)
	}
}

func TestStoppedMissingEntryPreservesSpecificLimitation(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].EntryPresent = false
	m.rows[0].Label = ""
	m.rows[0].Sandbox.Status = "stopped"
	m.rows[0].Status.Connection = "blocked"
	m.rows[0].Status.Detail = "This herdr version cannot add a stopped sandbox without connecting."

	hint, color := m.contextHint()
	require.False(t, hint != m.rows[0].Status.Detail)
	require.False(t, color != colorError)
}

func TestProductNamesInNoticesAndEvents(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.notice = "Herdr-sbx is waiting for Herdr"

	view := ansi.Strip(m.View().Content)
	require.Contains(t, view, "the plugin is waiting for herdr", "notice did not use hint wording: %s", view)

	m.events = []store.EventRecord{{Detail: "Herdr-sbx could not reach Herdr"}}
	var rendered string
	for _, line := range m.eventLines() {
		rendered += ansi.Strip(line.text)
	}
	require.Contains(t, rendered, "herdr-sbx could not reach herdr", "event retained product capitals: %s", rendered)
}

func TestErrorSectionFocusKeepsTabAndFooter(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height = 60, 18
	content := m.View().Content
	require.NotContains(t, content, "Errors ·")
	require.NotContains(t, content, "Events ·")
	require.NotContains(t, content, "Hint")
	m.addError(store.CodeSSHSetup, "Host", errNotice("missing configuration"))
	m.key(keyMsg('e', "e"))
	content = m.View().Content
	for _, want := range []string{"╭─ Errors · New (1) | All (1)", store.CodeSSHSetup + " · Host", "missing configuration"} {
		require.Contains(t, content, want, "missing %q in %s", want, content)
	}
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "errors", m.focus)
	lines := strings.Split(content, "\n")
	require.Len(t, lines, m.height)
	require.Contains(t, lines[len(lines)-2], "? help")
	for _, h := range m.hits {
		if h.action == "error" {
			clickAt(m, h.x, h.y)
			require.EqualValues(t, "errors", m.focus)
			require.EqualValues(t, "Sandboxes", m.page)
			h = selectionHit(t, m, "error", h.id)
			clickAt(m, h.x, h.y)
			require.Equal(t, "Status", m.page, "host error click did not open status recovery")
			return
		}
	}
	t.Fatal("error recovery not clickable")
}

func TestBlockedInputHintNamesEditAndAttemptedControl(t *testing.T) {
	m := modelFixture(t)
	m.input = &inputState{kind: "rename", target: "a", text: "private new label"}
	m.hitAction(hit{action: "page", id: "Plugins"})
	require.EqualValues(t, "Sandboxes", m.page)
	require.Contains(t, m.notice, "Renaming herdr label for alpha")
	require.Contains(t, m.notice, "before opening Plugins")
	require.NotContains(t, m.notice, "private new label")
	m.input = &inputState{kind: "filter", text: "secret filter"}
	m.hitAction(hit{action: "refresh"})
	require.Contains(t, m.notice, "Filtering Sandboxes")
	require.Contains(t, m.notice, "before refreshing observations")
	require.NotContains(t, m.notice, "secret filter")
	m.rows[0].Sandbox.Name = "alpha\x1b[31m\nline"
	m.hitAction(hit{action: "select", id: "a"})
	require.False(t, strings.ContainsAny(m.notice, "\x1b\n"))
	require.Contains(t, m.notice, "alpha[31m line")
	m.hitAction(hit{action: "plugin", id: "plugin\x1b[31m"})
	require.NotContains(t, m.notice, "\x1b")
	require.Contains(t, m.notice, "plugin[31m")
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
	require.EqualValues(t, "events", m.focus)
	require.False(t, m.eventsExpanded)
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEnd, ""))
	m.View()
	m.key(keyMsg(tea.KeyPgDown, ""))
	content := m.View().Content
	require.Contains(t, content, "Diagnostic event a", "oldest event not reachable: %s", content)
	m.key(keyMsg('e', "e"))
	content = m.View().Content
	require.Contains(t, content, store.CodeGuestPluginSync+" · guest.plugin", "active error not reachable: %s", content)
}

func TestGuestPluginErrorOpensAffectedSandbox(t *testing.T) {
	m := modelFixture(t)
	m.addError(store.CodeGuestPluginResponse, store.StatusKey("scope", "a"), errNotice("unexpected guest plugin response: expected 7 fields, got 8"))
	m.key(keyMsg('e', "e"))
	m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, "resync", m.sandboxActions(m.rows[0])[m.button].id)
}

type errNotice string

func (e errNotice) Error() string { return string(e) }
