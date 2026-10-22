package panel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
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
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{{PluginID: "first", Code: store.CodeGuestPluginSync, Detail: "build failed", Output: "compiler output", Recovery: "Repair source", Command: "herdr plugin install owner/plugin --yes", OccurredAt: now}, {PluginID: "second", Code: store.CodeGuestPluginSync, Detail: "build failed", OccurredAt: now}}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	m.focusErrors()
	if m.visibleErrors()[m.errorIndex].pluginID != "first" {
		t.Fatal("equal timestamps not sorted by plugin identity")
	}
	stale := m.pollWorker()().(workerMsg)
	m.key(keyMsg('d', "d"))
	if len(m.visibleErrors()) != 0 || m.focus != "diagnostics" {
		t.Fatal("dismissal did not hide the whole group")
	}
	persistPanelEffects(m)
	m.Update(stale)
	if len(m.visibleErrors()) != 0 {
		t.Fatal("stale poll undid dismissal")
	}
	m.Update(m.pollWorker()())
	reopened := newModel(nil)
	reopened.rows = m.rows
	reopened.loading = false
	reopened.workerLoaded = true
	reopened.Update(reopened.pollWorker()())
	if len(reopened.errors) != 2 || len(reopened.visibleErrors()) != 0 {
		t.Fatalf("dismissal missing after reopening: %+v", reopened.errors)
	}
	reopened.focusErrors()
	reopened.key(keyMsg('d', "d"))
	if reopened.focus != "diagnostics" || len(reopened.visibleErrors()) != 0 {
		t.Fatal("last dismissal did not focus title")
	}
	title := ansi.Strip(reopened.diagnosticTitle())
	if !strings.Contains(title, "New (0) | All (2)") || !strings.Contains(title, "2 active hidden") {
		t.Fatal(title)
	}
	reopened.key(keyMsg(tea.KeySpace, " "))
	if !reopened.showAllErrors || len(reopened.visibleErrors()) != 1 {
		t.Fatal("empty New title did not switch to all active diagnostics")
	}
}

func TestDismissalFailureRestoresNewAndReportsFailure(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeRename, scope: "a", detail: "name conflict", firstSeen: time.Now()}}
	m.focusErrors()
	m.dismissError()
	if len(m.effects) != 1 {
		t.Fatal("missing dismissal write")
	}
	m.effects[0].apply = func() error { return fmt.Errorf("disk full") }
	persistPanelEffects(m)
	if m.errors[0].dismissed {
		t.Fatal("failed dismissal hid active diagnostic")
	}
	found := false
	for _, e := range m.errors {
		if e.code == store.CodeDiagnosticDismiss {
			found = true
		}
	}
	if !found {
		t.Fatal("failed dismissal has no actionable diagnostic")
	}
	m.focusErrors()
	m.dismissError()
	persistPanelEffects(m)
	for _, e := range m.errors {
		if e.code == store.CodeDiagnosticDismiss {
			t.Fatal("successful dismissal retry left stale save failure")
		}
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
		if got.code != selected.code || got.scope != selected.scope {
			t.Fatal("poll moved group selection")
		}
	}
	m.dismissError()
	for _, e := range m.errors {
		if e.dismissed != (e.code == selected.code && e.scope == selected.scope) {
			t.Fatal("dismissed another group")
		}
	}
}

func TestDiagnosticMetadataUsesMutedTimeAndRepeat(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 110
	at := time.Date(2026, 10, 16, 12, 3, 4, 0, time.Local)
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "plugin", count: 12, seen: at}}
	header := m.errorLines()[1].text
	if !strings.Contains(header, "alpha · "+ink(colorMuted, "2026-10-16 12:03:04")+" "+ink(colorMuted, "×12")) {
		t.Fatal(header)
	}
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
		if !strings.Contains(title, ink(colorAccent, "\x1b[1m"+active+"\x1b[22m")) || !strings.Contains(title, ink(colorMuted, inactive)) {
			t.Fatalf("incorrect switch styling: %q", title)
		}
	}
	m.focusErrors()
	if help := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(help, "d dismiss") {
		t.Fatalf("dismiss hotkey missing: %q", help)
	}
	m.key(keyMsg('d', "d"))
	if !m.errors[0].dismissed {
		t.Fatal("d did not dismiss the selected diagnostic")
	}
}

func TestEventsPrecedeDiagnosticsAndCollapsedHeaderTraverses(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 40
	m.selected = "b"
	m.events = []store.EventRecord{{Code: store.CodePolicyCleanup, Scope: "Host", Detail: "Temporary access cleanup checked", Resolved: true}}
	m.errors = navigationErrors()
	view := ansi.Strip(m.View().Content)
	if strings.Index(view, "Events") > strings.Index(view, "Errors") || strings.Contains(view, "Temporary access cleanup checked") {
		t.Fatal("Events order or collapsed default is wrong")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "events" || m.eventIndex != -1 {
		t.Fatal("main table skipped Events")
	}
	m.key(keyMsg(tea.KeySpace, " "))
	if !m.eventsExpanded || !strings.Contains(ansi.Strip(m.View().Content), "Temporary access cleanup checked") {
		t.Fatal("Events header cannot expand")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "events" || m.eventIndex != 0 {
		t.Fatal("Events entry inaccessible")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "diagnostics" {
		t.Fatal("Events did not lead to diagnostic switch")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	if m.focus != "events" || m.eventIndex != 0 {
		t.Fatal("Up did not return to Events")
	}
	m.hitAction(hit{action: "event-section"})
	if m.eventsExpanded {
		t.Fatal("mouse header cannot collapse Events")
	}
}

func TestCleanupEventHasReadableContextAndNoCompletedHint(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	scope := store.ScopeID("/tmp/test-cleanup.sock")
	if err := store.RecordPolicyCleanupChecked(scope); err != nil {
		t.Fatal(err)
	}
	m.notice, m.noticeTarget = "Checking temporary access cleanup…", "policy-cleanup"
	m.pending["policy-cleanup"] = true
	m.Update(operationMsg{target: "policy-cleanup", code: store.CodePolicyCleanup})
	m.Update(m.pollWorker()())
	m.eventsExpanded = true
	view := ansi.Strip(m.View().Content)
	if m.notice != "" || strings.Contains(view, scope) || !strings.Contains(view, "Temporary access cleanup checked") || !strings.Contains(view, "All sandboxes") {
		t.Fatalf("cleanup event context or completed Hint is wrong: %s", view)
	}
}

func TestLocalDiagnosticWaitsForPersistedIdentityBeforeDismissal(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.addError(store.CodeRename, "a", fmt.Errorf("name taken"))
	m.focusErrors()
	before := len(m.effects)
	m.dismissError()
	if len(m.effects) != before || m.errors[0].dismissed || !strings.Contains(m.notice, "Saving this diagnostic") {
		t.Fatal("local unsaved cause accepted dismissal")
	}
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	m.dismissError()
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	if len(m.errors) != 1 || !m.errors[0].dismissed {
		t.Fatal("persisted cause could not be dismissed")
	}
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
	if len(groups) != 1 || groups[0].count != 5 || len(groups[0].entries) != 2 || !groups[0].seen.Equal(at.Add(time.Second)) {
		t.Fatalf("incorrect group: %+v", groups)
	}
	if title := ansi.Strip(m.diagnosticTitle()); !strings.Contains(title, "New (5) | All (6)") || !strings.Contains(title, "1 active hidden") {
		t.Fatal(title)
	}
	var body strings.Builder
	for _, line := range m.errorLines() {
		body.WriteString(ansi.Strip(line.text) + "\n")
		for _, h := range line.hits {
			if h.action == "error" && h.id != "0" {
				t.Fatalf("child gained separate focus: %+v", h)
			}
		}
	}
	text := body.String()
	if strings.Count(text, "P0002") != 1 || !strings.Contains(text, "×5") || !strings.Contains(text, "a1b2c3 first cause") || !strings.Contains(text, "d4e5f6 second cause") {
		t.Fatal(text)
	}
	m.focusErrors()
	m.key(keyMsg(tea.KeyDown, ""))
	if m.errorIndex != 0 {
		t.Fatal("Down entered a plugin detail")
	}
	m.recoverError()
	if m.selected != "a" || m.focus != "actions" {
		t.Fatal("group did not target sandbox recovery")
	}
}

func TestDismissedGroupIncludesNewPluginUntilFullRecovery(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	code := store.CodeGuestPluginSync
	failure := store.PluginFailure{PluginID: "first", Code: code, Detail: "first cause"}
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	m.focusErrors()
	m.dismissError()
	// A new child arrives before the acknowledgement write completes. The
	// optimistic group overlay hides it too, without dropping its details.
	stale := m.pollWorker()().(workerMsg)
	stale.errors["new"] = store.ErrorRecord{Code: code, Scope: "a", PluginID: "second", Detail: "second cause", FirstSeen: time.Now()}
	m.mergeErrors(stale)
	if len(m.errors) != 2 || len(m.visibleErrors()) != 0 {
		t.Fatal("new child escaped in-flight group dismissal")
	}
	persistPanelEffects(m)
	failure.PluginID, failure.Detail = "second", "changed cause"
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	if len(m.errors) != 1 || len(m.visibleErrors()) != 0 {
		t.Fatal("changed child reappeared while group remained active")
	}
	if err := store.SetGuestPluginFailures("a", nil); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	if len(m.errors) != 0 {
		t.Fatal("full recovery did not clear group")
	}
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	if len(m.visibleErrors()) != 1 {
		t.Fatalf("new episode after recovery stayed hidden: errors=%+v changes=%+v", m.errors, m.errorChanges)
	}
}

func TestLocalFailureInheritsActiveGroupDismissal(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "first", detail: "first cause", dismissed: true}}
	m.addError(store.CodeGuestPluginSync, "a", fmt.Errorf("local retry failed"))
	if len(m.errors) != 2 || !m.errors[1].dismissed || len(m.visibleErrors()) != 0 {
		t.Fatal("new local cause resurfaced an acknowledged group")
	}
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
		if len(groups) != 3 || groups[0].scope != "b" || groups[0].code != store.CodeRename || groups[1].scope != "b" || groups[1].code != store.CodeGuestPluginSync || groups[2].scope != "a" {
			t.Fatalf("groups did not follow sandbox name and code: %+v", groups)
		}
	}
	assertOrder()
	m.focusErrors()
	m.errorIndex = 2
	delete(entries, "old")
	m.mergeErrors(workerMsg{errors: entries})
	assertOrder()
	if m.errorIndex != 2 || m.visibleErrors()[m.errorIndex].scope != "a" {
		t.Fatal("member recovery moved group selection")
	}
}
