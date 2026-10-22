package panel

import (
	"errors"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestFailedResyncRecordsInfoEventWithoutReplacingPluginDiagnostic(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{{Code: store.CodeGuestPluginSync, PluginID: "example.plugin", Detail: "dependency missing"}}))
	m.Update(m.pollWorker()())
	_, command := m.Update(operationMsg{target: "a", code: store.CodePreparation, err: errors.New("dependency missing")})
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].code != store.CodeGuestPluginSync)
	require.Len(t, m.events, 2)

	event := m.events[1]
	require.False(t, event.Code != store.CodePreparation)
	require.False(t, store.SeverityForCode(event.Code) != store.SeverityInfo)
	require.Contains(t, event.Detail, "Re-sync did not finish: dependency missing")

	for _, message := range commandMessages(command) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(m.pollWorker()())
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 1)
	require.Len(t, saved.Events, 2)
	require.Len(t, m.errors, 1)
}
