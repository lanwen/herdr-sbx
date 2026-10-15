package panel

import (
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestFailedResyncRecordsInfoEventWithoutReplacingPluginDiagnostic(t *testing.T) {
	m := modelFixture(t)
	m.refreshing, m.workerLoaded = true, true
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{{Code: store.CodeGuestPluginSync, PluginID: "example.plugin", Detail: "dependency missing"}}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	_, command := m.Update(operationMsg{target: "a", code: store.CodePreparation, err: errors.New("dependency missing")})
	if len(m.errors) != 1 || m.errors[0].code != store.CodeGuestPluginSync || len(m.events) != 2 {
		t.Fatalf("re-sync replaced the actual failure: diagnostics=%+v events=%+v", m.errors, m.events)
	}
	if event := m.events[1]; event.Code != store.CodePreparation || store.SeverityForCode(event.Code) != store.SeverityInfo || !strings.Contains(event.Detail, "Re-sync did not finish: dependency missing") {
		t.Fatalf("missing informational re-sync outcome: %+v", event)
	}
	for _, message := range commandMessages(command) {
		if persisted, ok := message.(errorsPersistedMsg); ok {
			m.Update(persisted)
		}
	}
	m.Update(m.pollWorker()())
	saved, err := store.ReadStatusStore()
	if err != nil || len(saved.Errors) != 1 || len(saved.Events) != 2 || len(m.errors) != 1 {
		t.Fatalf("persisted outcome: %+v, %v", saved, err)
	}
}
