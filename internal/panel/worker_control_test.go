package panel

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
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
		require.NotNil(t, stop, "OFF did not request cancellation")
		control, _ := store.ReadStatusStore()
		require.NotNil(t, control.WorkerControl)
		require.True(t, control.WorkerControl.Stopped)

		command := m.setWorkerEnabled(true)
		require.Nil(t, command)
		require.True(t, m.workerEnabled)
		require.True(t, m.managementCancelling())

		got := ansi.Strip(m.managementStatus())
		require.Equal(t, "Cancelling", got, got)

		m.key(keyMsg('p', "p"))
		require.Equal(t, "Plugins", m.page, "navigation blocked")
		_, next := m.Update(stop())
		if stopFailure {
			require.EqualValues(t, 0, starts)
			require.False(t, m.pending["worker"])
			require.True(t, m.workerControlError)
			require.True(t, m.managementCancelling())
			require.Len(t, m.errors, 1)
			require.False(t, m.errors[0].code != store.CodeDaemonUnreachable)
			stopFailure = false
			retry := m.setWorkerEnabled(true)
			require.NotNil(t, retry)
			require.False(t, m.workerCommandEnabled)
			_, next = m.Update(retry())
		}
		require.NotNil(t, next)
		require.True(t, m.pending["worker"])
		require.True(t, m.workerEnabled)
		m.Update(next())
		require.EqualValues(t, 1, starts)
		require.False(t, m.pending["worker"])
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
	require.Nil(t, m.setWorkerEnabled(false))
	require.False(t, m.workerEnabled)
	require.True(t, m.managementCancelling())
	_, stop := m.Update(start())
	require.NotNil(t, stop)
	require.Len(t, m.errors, 0)
	m.Update(stop())
	saved, _ := store.ReadStatusStore()
	require.EqualValues(t, 1, stops)
	require.True(t, saved.WorkerControl.Stopped)
	require.False(t, m.workerEnabled)
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
	require.EqualValues(t, 0, starts)
	require.False(t, m.workerEnabled)
	require.False(t, m.pending["worker"])
}

func TestStopConfirmationReadFailureHasUnreachableCode(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.worker.State = "running"
	m.stopRequestedAt = time.Now()
	failure := fmt.Errorf("%w: permission denied reading daemon lock", lifecycle.ErrDaemonStopUnconfirmed)
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, err: failure})
	require.True(t, m.workerControlError)
	require.True(t, m.managementCancelling())
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].code != store.CodeDaemonUnreachable)
	require.Contains(t, m.errors[0].detail, "permission denied")
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
		require.Nil(t, cmd)
		require.False(t, m.workerEnabled)
		require.EqualValues(t, "", m.dialog)
		require.True(t, m.pending["worker"])
		require.True(t, m.managementCancelling())
		_, stop := m.Update(start())
		require.NotNil(t, stop, "queued OFF lost")
		m.Update(stop())
	}
}

func TestExplicitUnchangedOffPersistsBeforeFirstPoll(t *testing.T) {
	m := modelFixture(t)
	m.setWorkerEnabled(false)
	_, cmd := m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}})
	saved, _ := store.ReadStatusStore()
	require.Nil(t, cmd)
	require.False(t, m.workerEnabled)
	require.EqualValues(t, "", m.dialog)
	require.NotNil(t, saved.WorkerControl)
	require.True(t, saved.WorkerControl.Stopped)
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
	require.NotNil(t, stop)
	require.False(t, m.workerCommandEnabled)
	require.True(t, m.workerEnabled)
	require.True(t, m.managementCancelling())
	m.startWorkerFn = func() error { starts++; return nil }
	_, restart := m.Update(stop())
	require.NotNil(t, restart, "settled cancellation lost queued ON")
	m.Update(restart())
	require.EqualValues(t, 1, stops)
	require.EqualValues(t, 2, starts)
	require.False(t, m.pending["worker"])
	require.True(t, m.workerEnabled)
}
