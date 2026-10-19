package panel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func awaitWorkerCommand(t *testing.T, result <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-result:
		return msg
	case <-time.After(time.Second):
		t.Fatal("daemon command did not reach its test barrier")
		return nil
	}
}

func TestOnDuringDelayedStopQueuesExactlyOneReplacement(t *testing.T) {
	for _, stopError := range []error{nil, context.DeadlineExceeded} {
		t.Run(func() string {
			if stopError != nil {
				return "superseded stop failure"
			}
			return "stop success"
		}(), func(t *testing.T) {
			m := modelFixture(t)
			m.workerLoaded, m.refreshing, m.workerEnabled = true, true, true
			m.worker.State = "running"
			entered, release, results := make(chan tea.Msg, 1), make(chan struct{}), make(chan tea.Msg, 1)
			defer close(release)
			m.stopWorkerFn = func(context.Context) error {
				entered <- true
				<-release
				return stopError
			}
			starts := 0
			m.startWorkerFn = func() error { starts++; return nil }
			stop := m.setWorkerEnabled(false)
			go func() { results <- stop() }()
			awaitWorkerCommand(t, entered)
			if command := m.setWorkerEnabled(true); command != nil || !m.workerEnabled || starts != 0 {
				t.Fatal("ON did not queue visibly behind the unfinished stop")
			}
			if !strings.Contains(m.notice, "stop before starting") || !strings.Contains(ansi.Strip(m.autoSyncStatus()), "Stopping") {
				t.Fatal("queued ON lost its waiting hint or observed stopping transition")
			}
			m.key(keyMsg(tea.KeyRight, ""))
			m.key(keyMsg(tea.KeyRight, ""))
			if m.page != "Plugins" {
				t.Fatal("waiting for stop blocked navigation")
			}
			m.Update(workerMsg{status: store.WorkerStatus{State: "running"}})
			m.stopRequestedAt = time.Now().Add(-2 * time.Minute)
			m.Update(tickMsg(time.Now()))
			if !m.workerEnabled || m.workerControlError || len(m.errors) != 0 {
				t.Fatal("old stop polling/timeout replaced the queued ON choice with an error")
			}
			release <- struct{}{}
			_, start := m.Update(awaitWorkerCommand(t, results))
			if start == nil || !m.pending["worker"] || !m.workerEnabled || m.workerControlError || len(m.errors) != 0 {
				t.Fatal("superseded stop completion did not start the queued replacement cleanly")
			}
			if !strings.Contains(m.notice, "Starting auto-sync") {
				t.Fatalf("old stop completion replaced the new start notice: %q", m.notice)
			}
			if again := m.setWorkerEnabled(true); again != nil {
				t.Fatal("repeated ON scheduled another start")
			}
			m.Update(start())
			if starts != 1 || m.pending["worker"] || !m.workerEnabled {
				t.Fatal("queued ON did not finish exactly one replacement start")
			}
		})
	}
}

func TestOffDuringDelayedStartStopsAfterItFinishes(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded, m.refreshing = true, true
	m.worker.State = "stopped"
	entered, release, results := make(chan tea.Msg, 1), make(chan struct{}), make(chan tea.Msg, 1)
	defer close(release)
	m.startWorkerFn = func() error {
		entered <- true
		<-release
		return nil
	}
	stops := 0
	m.stopWorkerFn = func(context.Context) error { stops++; return nil }
	start := m.setWorkerEnabled(true)
	go func() { results <- start() }()
	awaitWorkerCommand(t, entered)
	if command := m.setWorkerEnabled(false); command != nil || m.workerEnabled || stops != 0 {
		t.Fatal("OFF did not queue behind the unfinished start")
	}
	if !strings.Contains(m.notice, "start before stopping") {
		t.Fatal("queued OFF did not explain that start is still in progress")
	}
	release <- struct{}{}
	_, stop := m.Update(awaitWorkerCommand(t, results))
	if stop == nil || m.workerEnabled || !strings.Contains(m.notice, "Stopping auto-sync") {
		t.Fatal("old start completion replaced the queued OFF choice or notice")
	}
	m.Update(stop())
	if stops != 1 || m.workerEnabled || m.pending["worker"] {
		t.Fatal("queued OFF did not finish exactly one stop")
	}
}

func TestRepeatedChoicesKeepOnlyLatestDaemonIntent(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded, m.refreshing, m.workerEnabled = true, true, true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return nil }
	starts := 0
	m.startWorkerFn = func() error { starts++; return nil }
	stop := m.setWorkerEnabled(false)
	m.setWorkerEnabled(true)
	m.setWorkerEnabled(false)
	if m.workerEnabled || !strings.Contains(m.notice, "Stopping auto-sync") {
		t.Fatal("returning to OFF kept the stale queued start hint")
	}
	m.Update(stop())
	if starts != 0 || m.workerEnabled || m.pending["worker"] {
		t.Fatal("cancelled queued ON started a replacement")
	}
}

func TestQueuedStartReportsItsOwnFailureInsteadOfSupersededStop(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded, m.refreshing, m.workerEnabled = true, true, true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return context.DeadlineExceeded }
	m.startWorkerFn = func() error { return errors.New("replacement start failed") }
	stop := m.setWorkerEnabled(false)
	m.setWorkerEnabled(true)
	_, start := m.Update(stop())
	if start == nil {
		t.Fatal("superseded stop did not schedule queued start")
	}
	m.Update(start())
	if !m.workerEnabled || !m.workerControlError || len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonStart || m.errors[0].detail != "replacement start failed" {
		t.Fatalf("queued start failure was replaced by stale stop feedback: %+v", m.errors)
	}
}

func TestStopConfirmationReadFailureHasUnreachableCode(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	failure := fmt.Errorf("%w: permission denied reading daemon lock", lifecycle.ErrDaemonStopUnconfirmed)
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, err: failure})
	if !m.workerControlError || len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonUnreachable || !strings.Contains(m.errors[0].detail, "permission denied") || !strings.Contains(m.errors[0].detail, "Cannot confirm") {
		t.Fatalf("unconfirmed stop lost its classification or diagnostic detail: %+v", m.errors)
	}
}

func TestDelayedFirstPollPreservesQueuedOff(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		m := modelFixture(t)
		m.refreshing = true
		starts, stops := 0, 0
		m.startWorkerFn = func() error { starts++; return nil }
		m.stopWorkerFn = func(context.Context) error { stops++; return nil }
		start := m.setWorkerEnabled(true)
		m.setWorkerEnabled(false)
		_, command := m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, initialized: initialized})
		if command != nil || m.workerEnabled || m.dialog != "" || !m.pending["worker"] || !strings.Contains(m.notice, "start before stopping") {
			t.Fatal("delayed first poll overwrote explicit OFF with automatic startup")
		}
		_, stop := m.Update(start())
		if stop == nil || m.workerEnabled {
			t.Fatal("queued OFF disappeared after delayed first poll")
		}
		m.Update(stop())
		if starts != 1 || stops != 1 || m.workerEnabled || m.pending["worker"] {
			t.Fatal("delayed first poll caused extra daemon commands")
		}
	}
}

func TestExplicitUnchangedOffPreventsFirstUseStartup(t *testing.T) {
	m := modelFixture(t)
	m.setWorkerEnabled(false)
	_, command := m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}})
	if command != nil || m.workerEnabled || m.dialog != "" {
		t.Fatal("first-use startup ignored an explicit unchanged OFF choice")
	}
}
