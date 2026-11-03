package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCreatedPreparationTimeoutRetainsCreationIntent(t *testing.T) {
	f, adds, _ := newCreatedFixture(t)
	probes, unavailable := failInitialCreatedProbe(f)
	scheduler := f.scheduler()
	scheduler.handle(createdSandboxEvent())
	require.Equal(t, 1, *probes)
	require.Zero(t, *adds, "registration ran before its remote check succeeded")
	*unavailable = false
	scheduler.processCatchup(false)
	require.Equal(t, 1, *probes, "failed preparation retried before the periodic tick")
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "a temporary remote check failure lost the stopped creation intent")
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "successful retry created a duplicate connection")
}

func TestQueuedPreparationRetryRechecksInactiveChoices(t *testing.T) {
	for _, change := range []string{"deleted", "disabled", "Management OFF"} {
		t.Run(change, func(t *testing.T) {
			f, adds, _ := newCreatedFixture(t)
			probes, unavailable := failInitialCreatedProbe(f)
			scheduler := f.scheduler()
			scheduler.handle(createdSandboxEvent())
			require.Equal(t, 1, *probes)
			*unavailable = false
			switch change {
			case "deleted":
				require.True(t, scheduler.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "new", SandboxName: "demo"}))
			case "disabled":
				require.NoError(t, store.SetEnabled(f.scope, "new", store.Disabled))
			case "Management OFF":
				require.NoError(t, store.SetWorkerControl(true))
			}
			scheduler.processCatchup(true)
			require.Zero(t, *adds)
			require.Equal(t, 1, *probes, "inactive choice permitted another guest connection")
		})
	}
}

func failInitialCreatedProbe(f *lifecycleFixture) (*int, *bool) {
	baseRun := f.run
	probes, unavailable := 0, true
	f.run = func(ctx context.Context, command process.Command) (string, error) {
		if command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "__herdr_on_path__") {
			probes++
			if unavailable {
				return `sandbox "demo" is unavailable`, context.DeadlineExceeded
			}
		}
		return baseRun(ctx, command)
	}
	return &probes, &unavailable
}

func TestBusyCreatedEventIsRetriedOnPeriodicCatchup(t *testing.T) {
	f, adds, _ := newCreatedFixture(t)
	scheduler := f.scheduler()
	var out bytes.Buffer
	scheduler.out = &out
	release := holdRegistrationLock(t)
	scheduler.handle(createdSandboxEvent())
	started := createdSandboxEvent()
	started.Action = "started"
	scheduler.handle(started)
	scheduler.handle(createdSandboxEvent())
	require.Zero(t, *adds)
	release()
	scheduler.processCatchup(false)
	require.Zero(t, *adds, "busy event retried before the periodic tick")
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "stopped creation intent was lost during lock contention")
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "duplicate lifecycle events added a second machine")
	require.NotContains(t, out.String(), store.ErrStateLocked.Error())
}

func TestQueuedCreationRechecksDeletionAndDisabledChoice(t *testing.T) {
	for _, change := range []string{"deleted", "disabled", "Management OFF", "replaced"} {
		t.Run(change, func(t *testing.T) {
			f, adds, guests := newCreatedFixture(t)
			scheduler := f.scheduler()
			release := holdRegistrationLock(t)
			scheduler.handle(createdSandboxEvent())
			switch change {
			case "deleted":
				require.True(t, scheduler.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "new", SandboxName: "demo"}))
			case "disabled":
				require.NoError(t, store.SetEnabled(f.scope, "new", store.Disabled))
			case "Management OFF":
				require.NoError(t, store.SetWorkerControl(true))
			case "replaced":
				f.request = func(*http.Request) (*http.Response, error) {
					return lifecycleResponse(200, `[{"uuid":"replacement","name":"demo","status":"stopped"}]`), nil
				}
			}
			release()
			scheduler.processCatchup(true)
			require.Zero(t, *adds)
			require.Zero(t, *guests)
		})
	}
}

func TestFullEventQueueRetainsCreationIntent(t *testing.T) {
	f, adds, _ := newCreatedFixture(t)
	scheduler := f.scheduler()
	for range cap(scheduler.queue) {
		require.True(t, scheduler.enqueue(sbx.LifecycleEvent{Type: "sync", Action: "complete"}))
	}
	require.False(t, scheduler.enqueue(createdSandboxEvent()))
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "stream reconnect would lose creation intent when the queue is full")
}

func TestCreatedInventoryFailureRetainsCreationIntent(t *testing.T) {
	f, adds, _ := newCreatedFixture(t)
	readable := f.request
	f.request = func(*http.Request) (*http.Response, error) {
		return nil, errors.New("inventory temporarily unavailable")
	}
	scheduler := f.scheduler()
	scheduler.handle(createdSandboxEvent())
	require.Zero(t, *adds)
	f.request = readable
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "temporary inventory failure lost the stopped creation intent")
}

func TestCreatedInventoryFailureDuringRegistrationRetainsIntent(t *testing.T) {
	f, adds, _ := newCreatedFixture(t)
	readable := f.request
	reads := 0
	f.request = func(request *http.Request) (*http.Response, error) {
		reads++
		if reads == 1 {
			return readable(request)
		}
		return nil, errors.New("inventory failed after initial identity check")
	}
	scheduler := f.scheduler()
	scheduler.handle(createdSandboxEvent())
	require.Zero(t, *adds)
	f.request = readable
	scheduler.processCatchup(true)
	require.Equal(t, 1, *adds, "an inventory failure during preparation lost creation intent")
}

func TestBusyCatchupWaitsWithoutMakingObservationsStale(t *testing.T) {
	f, _, _ := newCreatedFixture(t)
	require.NoError(t, store.RecordSandboxStatus(f.scope, sbx.Sandbox{ID: "new", Name: "demo", Status: "stopped"}, "not registered", "observed", false))
	scheduler := f.scheduler()
	scheduler.catchup, scheduler.freshCatchup = true, true
	var out bytes.Buffer
	scheduler.out = &out
	release := holdRegistrationLock(t)
	scheduler.processCatchup(false)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.False(t, status.Sandboxes[store.StatusKey(f.scope, "new")].Stale)
	require.True(t, scheduler.catchup)
	require.True(t, scheduler.freshCatchup)
	require.NotContains(t, out.String(), store.ErrStateLocked.Error())
	release()
	scheduler.processCatchup(false)
	require.True(t, scheduler.catchup, "an unrelated event retried a busy catch-up")
	scheduler.processCatchup(true)
	require.False(t, scheduler.catchup)
	require.False(t, scheduler.freshCatchup, out.String())
}

func holdRegistrationLock(t *testing.T) func() {
	t.Helper()
	unlock, err := store.LockState()
	require.NoError(t, err)
	var once sync.Once
	release := func() { once.Do(unlock) }
	t.Cleanup(release)
	return release
}
