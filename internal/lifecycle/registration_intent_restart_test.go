package lifecycle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCreatedRegistrationSurvivesWorkerRestartBeforeAdd(t *testing.T) {
	for _, mode := range []string{"probe timeout", "creating", "pending", "not visible", "inventory unavailable"} {
		t.Run(mode, func(t *testing.T) { testCreationRestart(t, mode) })
	}
}

func testCreationRestart(t *testing.T, mode string) {
	fixture, adds, _ := newCreatedFixture(t)
	ready := delayCreatedProvisioning(fixture, mode)
	first := fixture.scheduler()
	require.True(t, first.enqueue(createdSandboxEvent()))
	first.handle(<-first.queue)
	for range 3 {
		first.processCatchup(true)
	}
	require.Zero(t, *adds, "provisioning must not cause an early add")
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Contains(t, status.CreationRequests, store.StatusKey(fixture.scope, "new"))
	require.NotEqual(t, store.Disabled, status.EnabledChoices[store.StatusKey(fixture.scope, "new")])
	before, err := store.LoadState()
	require.NoError(t, err)
	for _, item := range before.Scopes[fixture.scope] {
		require.False(t, item.Pending, "no machine-add attempt has begun")
		require.Empty(t, item.ProfileID)
	}
	// Rebuild the service on the same state path, discarding all deferred memory.
	restartLifecycleService(fixture)
	*ready = true
	require.NoError(t, fixture.service.registrations.CaptureBaseline(context.Background(), "/tmp/lifecycle-test.sock"))
	restarted := fixture.scheduler()
	restarted.processCatchup(true)
	restarted.processCatchup(true)
	require.Equal(t, 1, *adds, "the requested stopped sandbox was forgotten across restart")
	saved, err := store.LoadState()
	require.NoError(t, err)
	require.Len(t, saved.Scopes[fixture.scope], 1)
	require.Equal(t, "machine-1", saved.Scopes[fixture.scope][0].ProfileID)
	require.False(t, saved.Scopes[fixture.scope][0].Pending)
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.Empty(t, status.CreationRequests)
}

func delayCreatedProvisioning(fixture *lifecycleFixture, mode string) *bool {
	ready := false
	originalRun, originalRequest := fixture.run, fixture.request
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		if !ready && mode == "probe timeout" && command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "__herdr_on_path__") {
			return "", process.ErrTimeout
		}
		return originalRun(ctx, command)
	}
	fixture.request = func(request *http.Request) (*http.Response, error) {
		if ready || mode == "probe timeout" {
			return originalRequest(request)
		}
		switch mode {
		case "inventory unavailable":
			return nil, fmt.Errorf("provisioning daemon temporarily unavailable")
		case "not visible":
			return lifecycleResponse(200, `[]`), nil
		default:
			return lifecycleResponse(200, fmt.Sprintf(`[{"uuid":"new","name":"demo","status":%q}]`, mode)), nil
		}
	}
	return &ready
}

func restartLifecycleService(fixture *lifecycleFixture) {
	runner := process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) { return fixture.run(ctx, command) }}
	sandboxClient := sbx.NewWithTransport(runner, lifecycleTransport(func(request *http.Request) (*http.Response, error) { return fixture.request(request) }))
	herdrClient := herdr.New(runner)
	inspector := precheck.New(sandboxClient, herdrClient, runner)
	registrations := registration.New(sandboxClient, herdrClient, inspector, runner)
	fixture.service = New(registrations, sandboxClient, herdrClient)
}

func TestCreatedRequestCancellationSurvivesRestart(t *testing.T) {
	for _, cause := range []string{"disabled", "deleted", "replaced"} {
		t.Run(cause, func(t *testing.T) {
			fixture, adds, _ := newCreatedFixture(t)
			ready := delayCreatedProvisioning(fixture, "pending")
			first := fixture.scheduler()
			first.handle(createdSandboxEvent())
			cancelCreatedRequest(t, fixture, first, cause)
			restartLifecycleService(fixture)
			restarted := fixture.scheduler()
			restarted.processCatchup(true)
			*ready = true
			restarted.processCatchup(true)
			require.Zero(t, *adds)
			status, err := store.ReadStatusStore()
			require.NoError(t, err)
			require.Empty(t, status.CreationRequests)
		})
	}
}

func cancelCreatedRequest(t *testing.T, fixture *lifecycleFixture, scheduler *lifecycleScheduler, cause string) {
	t.Helper()
	switch cause {
	case "disabled":
		require.NoError(t, store.SetEnabled(fixture.scope, "new", store.Disabled))
	case "deleted":
		require.True(t, scheduler.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "new", SandboxName: "demo"}))
	case "replaced":
		fixture.request = func(*http.Request) (*http.Response, error) {
			return lifecycleResponse(200, `[{"uuid":"replacement","name":"demo","status":"stopped"}]`), nil
		}
	}
}

func TestCreatedRequestPausesWhileManagementOffAcrossRestart(t *testing.T) {
	fixture, adds, guestCalls := newCreatedFixture(t)
	require.NoError(t, store.SetWorkerControl(true))
	first := fixture.scheduler()
	require.True(t, first.enqueue(createdSandboxEvent()))
	first.handle(<-first.queue)
	restartLifecycleService(fixture)
	restarted := fixture.scheduler()
	restarted.processCatchup(true)
	require.Zero(t, *adds)
	require.Zero(t, *guestCalls)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Contains(t, status.CreationRequests, store.StatusKey(fixture.scope, "new"))
	require.NoError(t, store.SetWorkerControl(false))
	restarted.processCatchup(true)
	require.Equal(t, 1, *adds)
}

func TestCreatedRequestDoesNotReplayUncertainAddAfterRestart(t *testing.T) {
	fixture, adds, _ := newCreatedFixture(t)
	original := fixture.run
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.HasPrefix(strings.Join(command.Args, " "), "machine add ") {
			(*adds)++
			return "Saved SSH machine machine-1. Remote server is ready.", nil
		}
		return original(ctx, command)
	}
	fixture.scheduler().handle(createdSandboxEvent())
	require.Equal(t, 1, *adds)
	before, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Contains(t, before.CreationRequests, store.StatusKey(fixture.scope, "new"))
	saved, err := store.LoadState()
	require.NoError(t, err)
	require.True(t, saved.Scopes[fixture.scope][0].Pending)
	restartLifecycleService(fixture)
	restarted := fixture.scheduler()
	for range 3 {
		restarted.processCatchup(true)
	}
	require.Equal(t, 1, *adds, "an uncertain attempted add must not regain creation permission")
}

func TestCreatedRequestSaveFailurePreventsGuestWork(t *testing.T) {
	fixture, adds, guestCalls := newCreatedFixture(t)
	path, err := store.StatePath()
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(path+".status", 0700))
	first := fixture.scheduler()
	require.True(t, first.enqueue(createdSandboxEvent()), "a saved-choice or persistence skip must not stop the event stream")
	require.Zero(t, *adds)
	require.Zero(t, *guestCalls)
	require.NoError(t, os.Remove(path+".status"))
	first.processCatchup(true)
	require.Equal(t, 1, *adds, "a failed request save should retain an in-memory retry")
}

func TestIgnoredCreatedEventDoesNotCloseLifecycleStream(t *testing.T) {
	for _, mode := range []string{"disabled", "retired", "name conflict", "save failure"} {
		t.Run(mode, func(t *testing.T) {
			fixture, adds, guestCalls := newCreatedFixture(t)
			scheduler := fixture.scheduler()
			skipCreation(t, fixture, scheduler, mode)
			fixture.request = func(*http.Request) (*http.Response, error) {
				return lifecycleResponse(200, "{\"type\":\"sandbox.lifecycle\",\"action\":\"created\",\"sandbox_id\":\"new\",\"sandbox_name\":\"demo\"}\n{\"type\":\"sync\",\"action\":\"complete\"}\n"), nil
			}
			delivered := 0
			err := fixture.service.sbx.ReadLifecycle(context.Background(), "/tmp/lifecycle-test.sock", func(event sbx.LifecycleEvent) bool {
				delivered++
				return scheduler.enqueue(event)
			})
			require.NoError(t, err)
			require.Equal(t, 2, delivered, "an ignored creation must not cancel the remaining stream")
			require.Zero(t, *adds)
			require.Zero(t, *guestCalls)
		})
	}
}

func skipCreation(t *testing.T, fixture *lifecycleFixture, scheduler *lifecycleScheduler, mode string) {
	t.Helper()
	switch mode {
	case "disabled":
		require.NoError(t, store.SetEnabled(fixture.scope, "new", store.Disabled))
	case "retired":
		scheduler.retired["new"] = true
	case "name conflict":
		accepted, err := store.RequestCreation(fixture.scope, "new", "original")
		require.NoError(t, err)
		require.True(t, accepted)
	case "save failure":
		path, err := store.StatePath()
		require.NoError(t, err)
		require.NoError(t, os.Mkdir(path+".status", 0700))
	}
}

func TestCreatedRequestIsDurableBeforeRegistrationLock(t *testing.T) {
	fixture, adds, _ := newCreatedFixture(t)
	unlock, err := store.LockState()
	require.NoError(t, err)
	first := fixture.scheduler()
	require.True(t, first.enqueue(createdSandboxEvent()))
	first.handle(<-first.queue)
	require.Zero(t, *adds)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Contains(t, saved.CreationRequests, store.StatusKey(fixture.scope, "new"))
	restartLifecycleService(fixture)
	unlock()
	fixture.scheduler().processCatchup(true)
	require.Equal(t, 1, *adds)
}

func TestCreatedRequestSurvivesInventoryFailureImmediatelyBeforeAdd(t *testing.T) {
	fixture, adds, _ := newCreatedFixture(t)
	original := fixture.request
	unavailable, hit := true, false
	fixture.request = func(request *http.Request) (*http.Response, error) {
		saved, err := store.LoadState()
		require.NoError(t, err)
		for _, item := range saved.Scopes[fixture.scope] {
			if unavailable && item.Pending {
				hit = true
				return nil, fmt.Errorf("inventory unavailable immediately before add")
			}
		}
		return original(request)
	}
	fixture.scheduler().handle(createdSandboxEvent())
	require.True(t, hit, "fixture must fail only after durable add intent")
	require.Zero(t, *adds)
	saved, err := store.LoadState()
	require.NoError(t, err)
	for _, item := range saved.Scopes[fixture.scope] {
		require.False(t, item.Pending, "known unperformed add must not remain uncertain")
	}
	restartLifecycleService(fixture)
	unavailable = false
	fixture.scheduler().processCatchup(true)
	require.Equal(t, 1, *adds, "pre-command observation failure lost the creation request")
}

func TestCancelCreatedRequestSurvivesUnavailableCatalog(t *testing.T) {
	fixture, adds, _ := newCreatedFixture(t)
	ready := delayCreatedProvisioning(fixture, "pending")
	fixture.scheduler().handle(createdSandboxEvent())
	rows, err := fixture.service.registrations.SandboxRows(context.Background(), "/tmp/lifecycle-test.sock")
	require.NoError(t, err)
	require.Equal(t, store.Enabled, rows[0].Enabled)
	original := fixture.run
	unavailable := true
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		if unavailable && command.Name == "herdr" && strings.Join(command.Args, " ") == "machine list --json" {
			return "", fmt.Errorf("saved connections temporarily unreadable")
		}
		return original(ctx, command)
	}
	require.Error(t, fixture.service.registrations.SetEnabled(context.Background(), fixture.scope, "new", store.Disabled))
	unavailable = false
	*ready = true
	fixture.request = func(*http.Request) (*http.Response, error) {
		return lifecycleResponse(200, `[{"uuid":"new","name":"demo","status":"running"}]`), nil
	}
	require.NoError(t, fixture.service.registrations.ReconcileContext(context.Background(), io.Discard))
	require.Zero(t, *adds, "cancelled creation was re-enabled after a failed host read")
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Empty(t, saved.CreationRequests)
	require.Equal(t, store.Disabled, saved.EnabledChoices[store.StatusKey(fixture.scope, "new")])
}
