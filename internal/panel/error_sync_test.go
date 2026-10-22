package panel

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// The old implementation replaced a local operation failure with a worker poll
// read before its persistence command had run.
func TestOperationErrorSurvivesStalePollUntilPersistenceIsObserved(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	stale := m.pollWorker()().(workerMsg)
	_, command := m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("label taken")})
	if m.notice != "" || m.focus != "table" {
		t.Fatalf("failure changed sandbox focus: %q / %s", m.notice, m.focus)
	}
	m.Update(stale)
	if len(m.errors) != 1 {
		t.Fatal("stale poll erased operation failure")
	}
	for _, message := range commandMessages(command) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(stale)
	if len(m.errors) != 1 {
		t.Fatal("late stale poll erased persisted failure")
	}
	fresh := m.pollWorker()().(workerMsg)
	m.Update(fresh)
	if len(m.errors) != 1 || m.errors[0].detail != "label taken" {
		t.Fatalf("persisted error missing: %+v", m.errors)
	}
	if len(m.errorChanges) != 0 {
		t.Fatal("confirmed local overlay was not released")
	}
	// A later confirming recovery may now clear the error.
	if err := store.ClearError(store.CodeRename, "a"); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	if len(m.errors) != 0 {
		t.Fatal("confirmed recovery did not clear error")
	}
	m.Update(fresh)
	if len(m.errors) != 0 {
		t.Fatal("older snapshot resurrected recovered error")
	}
}

func TestLocalOperationErrorsDoNotMaskOrClearPluginSiblings(t *testing.T) {
	m := modelFixture(t)
	code, scope := store.CodeGuestPluginSync, "a"
	plugins := map[string]store.ErrorRecord{
		"first":  {Code: code, Scope: scope, PluginID: "first.plugin", Detail: "same failure", Count: 2},
		"second": {Code: code, Scope: scope, PluginID: "second.plugin", Detail: "same failure", Count: 3},
	}
	observed := time.Now()
	m.mergeErrors(workerMsg{observedAt: observed, errors: plugins})
	m.addError(code, scope, errors.New("same failure"))
	m.mergeErrors(workerMsg{observedAt: observed.Add(time.Second), errors: plugins})
	if len(m.errors) != 3 {
		t.Fatalf("local operation masked plugin diagnostics: %+v", m.errors)
	}
	counts := map[string]int{}
	for _, entry := range m.errors {
		counts[entry.pluginID] = entry.count
	}
	if counts[""] != 1 || counts["first.plugin"] != 2 || counts["second.plugin"] != 3 {
		t.Fatalf("local operation changed independent counts: %+v", counts)
	}
	// Seed both siblings together, then persist local failure and recovery in
	// their original order. An operation recovery must not clear either plugin.
	if err := store.SetGuestPluginFailures(scope, []store.PluginFailure{{Code: code, PluginID: "first.plugin", Detail: "same failure"}, {Code: code, PluginID: "second.plugin", Detail: "same failure"}}); err != nil {
		t.Fatal(err)
	}
	m.clearError(code, scope)
	m.mergeErrors(workerMsg{observedAt: observed.Add(2 * time.Second), errors: plugins})
	if len(m.errors) != 2 {
		t.Fatalf("local recovery removed plugin siblings or resurrected local error: %+v", m.errors)
	}
	for _, effect := range m.effects {
		if err := effect.apply(); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := store.ReadStatusStore()
	if err != nil || len(saved.Errors) != 2 {
		t.Fatalf("local recovery cleared durable plugin siblings: %v %+v", err, saved.Errors)
	}
}

func TestUnwatchFailureKeepsSandboxNavigation(t *testing.T) {
	m := modelFixture(t)
	m.Update(operationMsg{target: "a", code: store.CodeManagement, err: errors.New("another reconciliation is already running")})
	if len(m.errors) != 1 || m.focus != "table" {
		t.Fatalf("error is missing or table focus was lost: %+v, %s", m.errors, m.focus)
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.selected != "b" {
		t.Fatalf("Down did not move to another sandbox: %s", m.selected)
	}
}

func TestErrorWritesPreserveOrderAcrossUpdates(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	_, first := m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("label taken")})
	_, second := m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "Saved"})
	// The recovery waits for the earlier failed-operation write.
	for _, message := range commandMessages(second) {
		if _, ok := message.(errorsPersistedMsg); ok {
			t.Fatal("error writes ran concurrently")
		}
	}
	var next tea.Cmd
	for _, message := range commandMessages(first) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			_, next = m.Update(persisted)
		}
	}
	stale := m.pollWorker()().(workerMsg)
	m.Update(stale)
	if len(m.errors) != 0 {
		t.Fatal("pending recovery was overwritten by persisted failure")
	}
	for _, message := range commandMessages(next) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(m.pollWorker()())
	if len(m.errors) != 0 {
		t.Fatal("serialized recovery did not clear error")
	}
	saved, err := store.ReadStatusStore()
	if err != nil || len(saved.Errors) != 0 {
		t.Fatalf("recovered error remains on disk: %v %+v", err, saved.Errors)
	}
}

func TestErrorHistoryRemainsReachableWithoutActiveErrors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.events = []store.EventRecord{{Code: store.CodeGuestPluginSync, Scope: "a", Detail: "dependency missing", At: time.Now()}}
	m.selected = "b"
	m.key(keyMsg(tea.KeyDown, ""))
	if m.page != "Sandboxes" || m.focus != "events" {
		t.Fatal("history navigation changed tab")
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEnd, ""))
	content := m.View().Content
	if !strings.Contains(content, "dependency missing") || len(strings.Split(content, "\n")) != m.height {
		t.Fatalf("small history view unusable: %s", content)
	}
}

func commandMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, commandMessages(child)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func TestNoticeTracksCurrentActionAndNavigation(t *testing.T) {
	m := modelFixture(t)
	m.notice = "herdr label saved"
	m.key(keyMsg('p', "p"))
	if m.notice != "" {
		t.Fatal("old success remained after changing tabs")
	}
	m.openTab("Sandboxes")
	m.notice = "herdr plugin choice saved"
	m.key(keyMsg(tea.KeyDown, ""))
	if m.notice != "" {
		t.Fatal("old success remained after changing sandbox")
	}
	m.notice = "Saved"
	m.addError(store.CodeSSHSetup, "Host", errors.New("missing key"))
	m.key(keyMsg('e', "e"))
	if m.notice != "" {
		t.Fatal("old success remained after focusing errors")
	}
	m.run("a", store.CodeRename, "herdr label saved", func() error { return nil })
	if !strings.Contains(m.notice, "Saving herdr label for alpha") {
		t.Fatalf("current action not named: %q", m.notice)
	}
	m.openTab("Plugins")
	if m.notice != "" {
		t.Fatal("navigation retained previous action hint")
	}
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "herdr label saved"})
	if m.notice != "" {
		t.Fatalf("completion appeared outside its action context: %q", m.notice)
	}
	m.key(keyMsg('s', "s"))
	if m.notice != "" {
		t.Fatal("completed feedback remained on navigation")
	}
}
