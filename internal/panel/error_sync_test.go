package panel

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

// The old implementation replaced a local operation failure with a worker poll
// read before its persistence command had run.
func TestOperationErrorSurvivesStalePollUntilPersistenceIsObserved(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	stale := m.pollWorker()().(workerMsg)
	_, command := m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("label taken")})
	require.EqualValues(t, "", m.notice)
	require.EqualValues(t, "table", m.focus)
	m.Update(stale)
	require.Len(t, m.errors, 1, "stale poll erased operation failure")
	for _, message := range commandMessages(command) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(stale)
	require.Len(t, m.errors, 1, "late stale poll erased persisted failure")
	fresh := m.pollWorker()().(workerMsg)
	m.Update(fresh)
	require.Len(t, m.errors, 1)
	require.EqualValues(t, "label taken", m.errors[0].detail)
	require.Len(t, m.errorChanges, 0, "confirmed local overlay was not released")
	// A later confirming recovery may now clear the error.
	require.NoError(t, store.ClearError(store.CodeRename, "a"))
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 0, "confirmed recovery did not clear error")
	m.Update(fresh)
	require.Len(t, m.errors, 0, "older snapshot resurrected recovered error")
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
	require.Len(t, m.errors, 3, "local operation masked plugin diagnostics: %+v", m.errors)
	counts := map[string]int{}
	for _, entry := range m.errors {
		counts[entry.pluginID] = entry.count
	}
	require.EqualValues(t, 1, counts[""])
	require.EqualValues(t, 2, counts["first.plugin"])
	require.EqualValues(t, 3, counts["second.plugin"])
	// Seed both siblings together, then persist local failure and recovery in
	// their original order. An operation recovery must not clear either plugin.
	require.NoError(t, store.SetGuestPluginFailures(scope, []store.PluginFailure{{Code: code, PluginID: "first.plugin", Detail: "same failure"}, {Code: code, PluginID: "second.plugin", Detail: "same failure"}}))
	m.clearError(code, scope)
	m.mergeErrors(workerMsg{observedAt: observed.Add(2 * time.Second), errors: plugins})
	require.Len(t, m.errors, 2, "local recovery removed plugin siblings or resurrected local error: %+v", m.errors)
	for _, effect := range m.effects {
		require.NoError(t, effect.apply())
	}
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 2)
}

func TestUnwatchFailureKeepsSandboxNavigation(t *testing.T) {
	m := modelFixture(t)
	m.Update(operationMsg{target: "a", code: store.CodeManagement, err: errors.New("another reconciliation is already running")})
	require.Len(t, m.errors, 1)
	require.EqualValues(t, "table", m.focus)
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "b", m.selected, "Down did not move to another sandbox: %s", m.selected)
}

func TestErrorWritesPreserveOrderAcrossUpdates(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	_, first := m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("label taken")})
	_, second := m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "Saved"})
	// The recovery waits for the earlier failed-operation write.
	for _, message := range commandMessages(second) {
		{
			_, ok := message.(errorsPersistedMsg)
			require.False(t, ok, "error writes ran concurrently")
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
	require.Len(t, m.errors, 0, "pending recovery was overwritten by persisted failure")
	for _, message := range commandMessages(next) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 0, "serialized recovery did not clear error")
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 0)
}

func TestErrorHistoryRemainsReachableWithoutActiveErrors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.events = []store.EventRecord{{Code: store.CodeGuestPluginSync, Scope: "a", Detail: "dependency missing", At: time.Now()}}
	m.selected = "b"
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "events", m.focus)
	m.key(keyMsg(tea.KeyEnter, ""))
	m.key(keyMsg(tea.KeyEnd, ""))
	content := m.View().Content
	require.Contains(t, content, "dependency missing")
	require.Len(t, strings.Split(content, "\n"), m.height)
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
	require.Equal(t, "", m.notice, "old success remained after changing tabs")
	m.openTab("Sandboxes")
	m.notice = "herdr plugin choice saved"
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "", m.notice, "old success remained after changing sandbox")
	m.notice = "Saved"
	m.addError(store.CodeSSHSetup, "Host", errors.New("missing key"))
	m.key(keyMsg('e', "e"))
	require.Equal(t, "", m.notice, "old success remained after focusing errors")
	m.run("a", store.CodeRename, "herdr label saved", func() error { return nil })
	require.Contains(t, m.notice, "Saving herdr label for alpha", "current action not named: %q", m.notice)
	m.openTab("Plugins")
	require.Equal(t, "", m.notice, "navigation retained previous action hint")
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "herdr label saved"})
	require.Equal(t, "", m.notice, "completion appeared outside its action context: %q", m.notice)
	m.key(keyMsg('s', "s"))
	require.Equal(t, "", m.notice, "completed feedback remained on navigation")
}
