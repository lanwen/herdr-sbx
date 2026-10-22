package panel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestOnWaitsForSuccessfulCancellation(t *testing.T) {
	for _, stopFailure := range []bool{false, true} {
		m := modelFixture(t)
		m.workerLoaded = true
		m.refreshing = true
		m.workerEnabled = true
		m.worker.State = "running"
		stops, starts := 0, 0
		m.stopWorkerFn = func(context.Context) error {
			stops++
			if stopFailure {
				return context.DeadlineExceeded
			}
			return nil
		}
		m.startWorkerFn = func() error { starts++; return nil }
		stop := m.setWorkerEnabled(false)
		if stop == nil {
			t.Fatal("OFF did not request cancellation")
		}
		control, _ := store.ReadStatusStore()
		if control.WorkerControl == nil || !control.WorkerControl.Stopped {
			t.Fatal("OFF was not persisted on acceptance")
		}
		if command := m.setWorkerEnabled(true); command != nil || !m.workerEnabled || !m.managementCancelling() {
			t.Fatal("ON bypassed cancellation")
		}
		if got := ansi.Strip(m.managementStatus()); got != "Cancelling" {
			t.Fatal(got)
		}
		m.key(keyMsg('p', "p"))
		if m.page != "Plugins" {
			t.Fatal("navigation blocked")
		}
		_, next := m.Update(stop())
		if stopFailure {
			if starts != 0 || m.pending["worker"] || !m.workerControlError || !m.managementCancelling() {
				t.Fatal("failed cancellation claimed settled or started worker")
			}
			if len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonUnreachable {
				t.Fatal(m.errors)
			}
			stopFailure = false
			retry := m.setWorkerEnabled(true)
			if retry == nil || m.workerCommandEnabled {
				t.Fatal("retry must confirm stop first")
			}
			_, next = m.Update(retry())
		}
		if next == nil || !m.pending["worker"] || !m.workerEnabled {
			t.Fatal("confirmed stop lost queued ON")
		}
		m.Update(next())
		if starts != 1 || m.pending["worker"] {
			t.Fatal("queued ON must start once")
		}
		_ = stops
	}
}

func TestOffInvalidatesDelayedStartAtAcceptance(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.workerLoaded = true
	m.worker.State = "stopped"
	stops := 0
	m.startWorkerFn = func() error {
		accepted, err := store.SetWorkerControlIfGeneration(false, m.workerStartGeneration)
		if err != nil {
			return err
		}
		if !accepted {
			return context.Canceled
		}
		return nil
	}
	m.stopWorkerFn = func(context.Context) error { stops++; return nil }
	start := m.setWorkerEnabled(true)
	if m.setWorkerEnabled(false) != nil || m.workerEnabled || !m.managementCancelling() {
		t.Fatal("OFF not accepted while start queued")
	}
	_, stop := m.Update(start())
	if stop == nil || len(m.errors) != 0 {
		t.Fatal("superseded start reported failure or dropped OFF")
	}
	m.Update(stop())
	saved, _ := store.ReadStatusStore()
	if stops != 1 || !saved.WorkerControl.Stopped || m.workerEnabled {
		t.Fatal("delayed start overwrote OFF")
	}
}

func TestRepeatedChoicesKeepLatestIntent(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.workerEnabled = true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return nil }
	starts := 0
	m.startWorkerFn = func() error { starts++; return nil }
	stop := m.setWorkerEnabled(false)
	m.setWorkerEnabled(true)
	m.setWorkerEnabled(false)
	m.Update(stop())
	if starts != 0 || m.workerEnabled || m.pending["worker"] {
		t.Fatal("cancelled ON started worker")
	}
}

func TestStopConfirmationReadFailureHasUnreachableCode(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.worker.State = "running"
	m.stopRequestedAt = time.Now()
	failure := fmt.Errorf("%w: permission denied reading daemon lock", lifecycle.ErrDaemonStopUnconfirmed)
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, err: failure})
	if !m.workerControlError || !m.managementCancelling() || len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonUnreachable || !strings.Contains(m.errors[0].detail, "permission denied") {
		t.Fatal(m.errors)
	}
}

func TestDelayedFirstPollPreservesQueuedOff(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		m := modelFixture(t)
		m.refreshing = true
		m.startWorkerFn = func() error { return nil }
		m.stopWorkerFn = func(context.Context) error { return nil }
		start := m.setWorkerEnabled(true)
		m.setWorkerEnabled(false)
		_, cmd := m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, initialized: initialized})
		if cmd != nil || m.workerEnabled || m.dialog != "" || !m.pending["worker"] || !m.managementCancelling() {
			t.Fatal("poll overwrote accepted OFF")
		}
		_, stop := m.Update(start())
		if stop == nil {
			t.Fatal("queued OFF lost")
		}
		m.Update(stop())
	}
}

func TestExplicitUnchangedOffPersistsBeforeFirstPoll(t *testing.T) {
	m := modelFixture(t)
	m.setWorkerEnabled(false)
	_, cmd := m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}})
	saved, _ := store.ReadStatusStore()
	if cmd != nil || m.workerEnabled || m.dialog != "" || saved.WorkerControl == nil || !saved.WorkerControl.Stopped {
		t.Fatal("first-use startup ignored explicit OFF")
	}
}

func TestOnOffOnBeforeStartRunsConfirmsCancellationFirst(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.worker.State = "stopped"
	starts, stops := 0, 0
	m.startWorkerFn = func() error { starts++; return context.Canceled }
	m.stopWorkerFn = func(context.Context) error { stops++; return nil }
	first := m.setWorkerEnabled(true)
	m.setWorkerEnabled(false)
	m.setWorkerEnabled(true)
	_, stop := m.Update(first())
	if stop == nil || m.workerCommandEnabled || !m.workerEnabled || !m.managementCancelling() {
		t.Fatal("new ON bypassed cancelled start")
	}
	m.startWorkerFn = func() error { starts++; return nil }
	_, restart := m.Update(stop())
	if restart == nil {
		t.Fatal("settled cancellation lost queued ON")
	}
	m.Update(restart())
	if stops != 1 || starts != 2 || m.pending["worker"] || !m.workerEnabled {
		t.Fatal("unexpected worker overlap")
	}
}
