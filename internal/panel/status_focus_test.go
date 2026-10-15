package panel

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestStatusFocusClampsWhenItsFinalActionDisappears(t *testing.T) {
	for _, change := range []string{"new sync", "SSH ready", "observed error recovery", "completed action"} {
		t.Run(change, func(t *testing.T) {
			m := modelFixture(t)
			m.page, m.workerEnabled, m.workerLoaded, m.refreshing = "Status", true, true, true
			m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
			m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready}}
			var message tea.Msg
			switch change {
			case "new sync":
				m.worker.LastSync = time.Now().Add(-time.Minute)
				message = workerMsg{status: store.WorkerStatus{State: "running", LastSync: time.Now()}}
			case "SSH ready":
				m.checks.Checks = nil
				message = checksMsg{checks: precheck.Result{Checks: []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready}}}}
			case "observed error recovery":
				m.errors = []uiError{{code: store.CodePluginDiscovery, scope: "Host", detail: "catalog unavailable"}}
				message = observationMsg{generation: m.generation}
			case "completed action":
				m.errors = []uiError{{code: store.CodePluginDiscovery, scope: "Host", detail: "catalog unavailable"}}
				message = operationMsg{target: "Host", code: store.CodePluginDiscovery}
			}
			m.button = len(m.statusActions()) - 1
			if m.button == 0 {
				t.Fatal("fixture has no disappearing action")
			}
			m.Update(message)
			if m.button != 0 || len(m.statusActions()) != 1 || !m.reveal {
				t.Fatalf("Status focus remained outside available actions: %d/%d", m.button, len(m.statusActions()))
			}
		})
	}
}

func TestStatusActionClampingPreservesDialogFocus(t *testing.T) {
	m := modelFixture(t)
	m.page, m.dialog, m.button = "Status", "startup", 1
	m.workerEnabled, m.workerLoaded = true, true
	m.worker = store.WorkerStatus{State: "running", LastSync: time.Now()}
	m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready}}
	m.Update(workerMsg{status: m.worker})
	if m.button != 1 || m.dialog != "startup" {
		t.Fatal("Status action shrink changed dialog focus")
	}
}
