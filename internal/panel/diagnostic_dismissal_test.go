package panel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func persistPanelEffects(m *model) {
	_, cmd := m.Update(nil)
	for cmd != nil {
		var next tea.Cmd
		for _, msg := range commandMessages(cmd) {
			if persisted, ok := msg.(errorsPersistedMsg); ok {
				_, next = m.Update(persisted)
			}
		}
		cmd = next
	}
}

func TestPluginDismissalPersistsAcrossPollsAndReopening(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	now := time.Now()
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{{PluginID: "first", Code: store.CodeGuestPluginSync, Detail: "build failed", Output: "compiler output", Recovery: "Repair source", Command: "herdr plugin install owner/plugin --yes", OccurredAt: now}, {PluginID: "second", Code: store.CodeGuestPluginSync, Detail: "build failed", OccurredAt: now}}))
	m.Update(m.pollWorker()())
	m.focusErrors()
	require.Equal(t, "first", m.visibleErrors()[m.errorIndex].pluginID, "equal timestamps not sorted by plugin identity")
	stale := m.pollWorker()().(workerMsg)
	m.key(keyMsg('d', "d"))
	require.Len(t, m.visibleErrors(), 0)
	require.EqualValues(t, "diagnostics", m.focus)
	persistPanelEffects(m)
	m.Update(stale)
	require.Len(t, m.visibleErrors(), 0, "stale poll undid dismissal")
	m.Update(m.pollWorker()())
	reopened := newModel(nil)
	reopened.rows = m.rows
	reopened.loading = false
	reopened.workerLoaded = true
	reopened.Update(reopened.pollWorker()())
	require.Len(t, reopened.errors, 2)
	require.Len(t, reopened.visibleErrors(), 0)
	reopened.focusErrors()
	reopened.key(keyMsg('d', "d"))
	require.EqualValues(t, "diagnostics", reopened.focus)
	require.Len(t, reopened.visibleErrors(), 0)
	title := ansi.Strip(reopened.diagnosticTitle())
	require.Contains(t, title, "New (0) | All (2)")
	require.Contains(t, title, "2 active hidden")
	reopened.key(keyMsg(tea.KeySpace, " "))
	require.True(t, reopened.showAllErrors)
	require.Len(t, reopened.visibleErrors(), 1)
}

func TestDismissalFailureRestoresNewAndReportsFailure(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeRename, scope: "a", detail: "name conflict", firstSeen: time.Now()}}
	m.focusErrors()
	m.dismissError()
	require.Len(t, m.effects, 1, "missing dismissal write")
	m.effects[0].apply = func() error { return fmt.Errorf("disk full") }
	persistPanelEffects(m)
	require.False(t, m.errors[0].dismissed, "failed dismissal hid active diagnostic")
	found := false
	for _, e := range m.errors {
		if e.code == store.CodeDiagnosticDismiss {
			found = true
		}
	}
	require.True(t, found, "failed dismissal has no actionable diagnostic")
	m.focusErrors()
	m.dismissError()
	persistPanelEffects(m)
	for _, e := range m.errors {
		require.NotEqual(t, store.CodeDiagnosticDismiss, e.code, "successful dismissal retry left stale save failure")
	}
}

func TestPollingPreservesSelectedGroupForDismissal(t *testing.T) {
	m := modelFixture(t)
	at := time.Now()
	entries := map[string]store.ErrorRecord{
		"one":   {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "first", Detail: "build failed", LastSeen: at, FirstSeen: at},
		"two":   {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "second", Detail: "build failed", LastSeen: at, FirstSeen: at},
		"other": {Code: store.CodeRename, Scope: "b", Detail: "label taken", LastSeen: at, FirstSeen: at},
	}
	m.mergeErrors(workerMsg{errors: entries})
	m.focusErrors()
	m.errorIndex = 1
	selected := m.visibleErrors()[m.errorIndex]
	for i := 0; i < 20; i++ {
		changed := entries["one"]
		changed.LastSeen = at.Add(time.Duration(i+1) * time.Second)
		entries["one"] = changed
		m.mergeErrors(workerMsg{errors: entries})
		got := m.visibleErrors()[m.errorIndex]
		require.False(t, got.code != selected.code)
		require.False(t, got.scope != selected.scope)
	}
	m.dismissError()
	for _, e := range m.errors {
		require.Equal(t, (e.code == selected.code && e.scope == selected.scope), e.dismissed, "dismissed another group")
	}
}

func TestDiagnosticMetadataUsesMutedTimeAndRepeat(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 110
	at := time.Date(2026, 10, 16, 12, 3, 4, 0, time.Local)
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "plugin", count: 12, seen: at}}
	header := m.errorLines()[1].text
	require.Contains(t, header, "alpha · "+ink(colorMuted, "2026-10-16 12:03:04")+" "+ink(colorMuted, "×12"), header)
}

func TestDiagnosticSwitchStylesAndKeyboardDismissal(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 110
	m.errors = []uiError{{ref: "a1b2c3", code: store.CodeRename, scope: "a", detail: "label taken", firstSeen: time.Now()}}
	for _, all := range []bool{false, true} {
		m.showAllErrors = all
		active, inactive := "New (1)", "All (1)"
		if all {
			active, inactive = inactive, active
		}
		title := m.diagnosticTitle()
		require.Contains(t, title, ink(colorAccent, "\x1b[1m"+active+"\x1b[22m"))
		require.Contains(t, title, ink(colorMuted, inactive))
	}
	m.focusErrors()

	help := ansi.Strip(m.renderHelp(110, false))
	require.Contains(t, help, "d dismiss", "dismiss hotkey missing: %q", help)

	m.key(keyMsg('d', "d"))
	require.True(t, m.errors[0].dismissed, "d did not dismiss the selected diagnostic")
}

func TestEventsPrecedeDiagnosticsAndCollapsedHeaderTraverses(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 40
	m.selected = "b"
	m.events = []store.EventRecord{{Code: store.CodePolicyCleanup, Scope: "Host", Detail: "Temporary access cleanup checked", Resolved: true}}
	m.errors = navigationErrors()
	view := ansi.Strip(m.View().Content)
	require.False(t, strings.Index(view, "Events") > strings.Index(view, "Errors"))
	require.NotContains(t, view, "Temporary access cleanup checked")
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "events", m.focus)
	require.False(t, m.eventIndex != -1)
	m.key(keyMsg(tea.KeySpace, " "))
	require.True(t, m.eventsExpanded)
	require.Contains(t, ansi.Strip(m.View().Content), "Temporary access cleanup checked")
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "events", m.focus)
	require.EqualValues(t, 0, m.eventIndex)
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "diagnostics", m.focus, "Events did not lead to diagnostic switch")
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "events", m.focus)
	require.EqualValues(t, 0, m.eventIndex)
	m.hitAction(hit{action: "event-section"})
	require.False(t, m.eventsExpanded, "mouse header cannot collapse Events")
}

func TestCleanupEventHasReadableContextAndNoCompletedHint(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	scope := store.ScopeID("/tmp/test-cleanup.sock")
	require.NoError(t, store.RecordPolicyCleanupChecked(scope))
	m.notice, m.noticeTarget = "Checking temporary access cleanup…", "policy-cleanup"
	m.pending["policy-cleanup"] = true
	m.Update(operationMsg{target: "policy-cleanup", code: store.CodePolicyCleanup})
	m.Update(m.pollWorker()())
	m.eventsExpanded = true
	view := ansi.Strip(m.View().Content)
	require.EqualValues(t, "", m.notice)
	require.NotContains(t, view, scope)
	require.Contains(t, view, "Temporary access cleanup checked")
	require.Contains(t, view, "All sandboxes")
}

func TestLocalDiagnosticWaitsForPersistedIdentityBeforeDismissal(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.addError(store.CodeRename, "a", fmt.Errorf("name taken"))
	m.focusErrors()
	before := len(m.effects)
	m.dismissError()
	require.Len(t, m.effects, before)
	require.False(t, m.errors[0].dismissed)
	require.Contains(t, m.notice, "Saving this diagnostic")
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	m.dismissError()
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 1)
	require.True(t, m.errors[0].dismissed)
}

func TestDiagnosticGroupCountsOccurrencesAndKeepsPluginDetails(t *testing.T) {
	m := modelFixture(t)
	m.width = 110
	at := time.Now().Truncate(time.Second)
	m.errors = []uiError{
		{code: store.CodeGuestPluginSync, scope: "a", pluginID: "first", ref: "a1b2c3", detail: "first cause", count: 3, seen: at},
		{code: store.CodeGuestPluginSync, scope: "a", pluginID: "second", ref: "d4e5f6", detail: "second cause", count: 2, seen: at.Add(time.Second)},
		{code: store.CodeRename, scope: "b", detail: "label taken", dismissed: true},
	}
	groups := m.visibleErrors()
	require.Len(t, groups, 1)
	require.EqualValues(t, 5, groups[0].count)
	require.Len(t, groups[0].entries, 2)
	require.True(t, groups[0].seen.Equal(at.Add(time.Second)))

	title := ansi.Strip(m.diagnosticTitle())
	require.Contains(t, title, "New (5) | All (6)")
	require.Contains(t, title, "1 active hidden")

	var body strings.Builder
	for _, line := range m.errorLines() {
		body.WriteString(ansi.Strip(line.text) + "\n")
		for _, h := range line.hits {
			require.False(t, h.action == "error" && h.id != "0", "child gained separate focus: %+v", h)
		}
	}
	text := body.String()
	require.EqualValues(t, 1, strings.Count(text, "P0002"))
	require.Contains(t, text, "×5")
	require.Contains(t, text, "a1b2c3 first cause")
	require.Contains(t, text, "d4e5f6 second cause")
	m.focusErrors()
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, 0, m.errorIndex, "Down entered a plugin detail")
	m.recoverError()
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "actions", m.focus)
}

func TestDismissedGroupIncludesNewPluginUntilFullRecovery(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	code := store.CodeGuestPluginSync
	failure := store.PluginFailure{PluginID: "first", Code: code, Detail: "first cause"}
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{failure}))
	m.Update(m.pollWorker()())
	m.focusErrors()
	m.dismissError()
	// A new child arrives before the acknowledgement write completes. The
	// optimistic group overlay hides it too, without dropping its details.
	stale := m.pollWorker()().(workerMsg)
	stale.errors["new"] = store.ErrorRecord{Code: code, Scope: "a", PluginID: "second", Detail: "second cause", FirstSeen: time.Now()}
	m.mergeErrors(stale)
	require.Len(t, m.errors, 2)
	require.Len(t, m.visibleErrors(), 0)
	persistPanelEffects(m)
	failure.PluginID, failure.Detail = "second", "changed cause"
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{failure}))
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 1)
	require.Len(t, m.visibleErrors(), 0)
	require.NoError(t, store.SetGuestPluginFailures("a", nil))
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 0, "full recovery did not clear group")
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{failure}))
	m.Update(m.pollWorker()())
	require.Len(t, m.visibleErrors(), 1, "new episode after recovery stayed hidden: errors=%+v changes=%+v", m.errors, m.errorChanges)
}

func TestLocalFailureInheritsActiveGroupDismissal(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "first", detail: "first cause", dismissed: true}}
	m.addError(store.CodeGuestPluginSync, "a", fmt.Errorf("local retry failed"))
	require.Len(t, m.errors, 2)
	require.True(t, m.errors[1].dismissed)
	require.Len(t, m.visibleErrors(), 0)
}

func TestDiagnosticGroupOrderUsesSandboxNameAndCode(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].Sandbox.Name = "Zeta"
	m.rows[1].Sandbox.Name = "alpha"
	at := time.Now()
	entries := map[string]store.ErrorRecord{
		"old":          {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "first", FirstSeen: at.Add(-time.Hour)},
		"new":          {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "second", FirstSeen: at.Add(time.Hour)},
		"alpha-plugin": {Code: store.CodeGuestPluginSync, Scope: "b", PluginID: "first", FirstSeen: at},
		"alpha-rename": {Code: store.CodeRename, Scope: "b", FirstSeen: at.Add(time.Hour)},
	}
	m.mergeErrors(workerMsg{errors: entries})
	assertOrder := func() {
		t.Helper()
		groups := m.visibleErrors()
		require.Len(t, groups, 3)
		require.EqualValues(t, "b", groups[0].scope)
		require.False(t, groups[0].code != store.CodeRename)
		require.EqualValues(t, "b", groups[1].scope)
		require.False(t, groups[1].code != store.CodeGuestPluginSync)
		require.EqualValues(t, "a", groups[2].scope)
	}
	assertOrder()
	m.focusErrors()
	m.errorIndex = 2
	delete(entries, "old")
	m.mergeErrors(workerMsg{errors: entries})
	assertOrder()
	require.EqualValues(t, 2, m.errorIndex)
	require.EqualValues(t, "a", m.visibleErrors()[m.errorIndex].scope)
}
