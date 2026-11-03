package lifecycle

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRecreatedRegistrationRecoversKnownUnsavedAddAfterRestart(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed Enable", true: "explicit Disable"}[cancel], func(t *testing.T) {
			testRecreatedRecovery(t, cancel)
		})
	}
}

func testRecreatedRecovery(t *testing.T, cancel bool) {
	fixture, successfulAdds, _ := newCreatedFixture(t)
	original := fixture.run
	ready, attempts := false, 0
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.HasPrefix(strings.Join(command.Args, " "), "machine add ") {
			attempts++
			if !ready {
				return "remote platform detection failed: sandbox identity is unavailable. Machine was not saved.", &exec.ExitError{}
			}
		}
		return original(ctx, command)
	}
	scheduler := fixture.scheduler()
	require.True(t, scheduler.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "old-identity", SandboxName: "demo"}))
	scheduler.handle(<-scheduler.queue)
	scheduler.handle(createdSandboxEvent())
	require.Equal(t, 2, attempts, "one operation retains only its existing bounded transient retry")
	require.Zero(t, *successfulAdds)
	// The later manual Enable follows the real incident's failed-settlement path.
	fixture.request = func(*http.Request) (*http.Response, error) {
		return lifecycleResponse(200, `[{"uuid":"new","name":"demo","status":"running"}]`), nil
	}
	require.Error(t, fixture.service.registrations.SetEnabled(context.Background(), fixture.scope, "new", store.Enabled))
	require.Equal(t, 4, attempts)
	if cancel {
		require.NoError(t, fixture.service.registrations.SetEnabled(context.Background(), fixture.scope, "new", store.Disabled))
	}
	restartLifecycleService(fixture)
	ready = true
	restarted := fixture.scheduler()
	restarted.processCatchup(true)
	restarted.processCatchup(true)
	want := 1
	if cancel {
		want = 0
	}
	require.Equal(t, want, *successfulAdds, "retained positive intent must recover, but explicit cancellation must not")
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Empty(t, saved.CreationRequests)
}

func TestUncertainRecreatedAddStaysInertAfterRestart(t *testing.T) {
	for _, mode := range []string{"unknown wording", "acknowledged", "catalog unavailable", "lost response", "candidate", "cancel during final check"} {
		t.Run(mode, func(t *testing.T) { testInertRecreatedAdd(t, mode) })
	}
}

func testInertRecreatedAdd(t *testing.T, mode string) {
	fixture, successfulAdds, _ := newCreatedFixture(t)
	original := fixture.run
	ready, attempts, reads := false, 0, 0
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		if !ready && call == "herdr machine list --json" && attempts > 0 {
			reads++
			cancelOnFinalCreatedCheck(t, fixture, mode, attempts, reads)
		}
		if !ready && mode == "catalog unavailable" && attempts > 0 && call == "herdr machine list --json" {
			return "", fmt.Errorf("saved connections unavailable")
		}
		if strings.HasPrefix(call, "herdr machine add ") {
			attempts++
			reads = 0
			if !ready {
				return recreatedAddFailure(fixture, mode)
			}
		}
		return original(ctx, command)
	}
	fixture.scheduler().handle(createdSandboxEvent())
	before := attempts
	if mode == "acknowledged" {
		require.Equal(t, 1, before, "an acknowledged attempt must not be repeated")
	}
	require.Zero(t, *successfulAdds)
	restartLifecycleService(fixture)
	ready = true
	fixture.catalog = `[]`
	restarted := fixture.scheduler()
	for range 3 {
		restarted.processCatchup(true)
	}
	require.Equal(t, before, attempts, "an uncertain result cannot reactivate the retained request")
	saved, err := store.LoadState()
	require.NoError(t, err)
	require.True(t, saved.Scopes[fixture.scope][0].Pending)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	if mode == "cancel during final check" {
		require.Empty(t, status.CreationRequests)
	} else {
		require.Contains(t, status.CreationRequests, store.StatusKey(fixture.scope, "new"))
	}
}

func recreatedAddFailure(fixture *lifecycleFixture, mode string) (string, error) {
	output := "remote platform detection failed: sandbox identity is unavailable. Machine was not saved."
	switch mode {
	case "unknown wording":
		output = "remote platform detection failed: sandbox identity is unavailable"
	case "acknowledged":
		output = "Saved SSH machine machine-1. Remote server is ready.\n" + output
	case "lost response":
		return output, process.ErrTimeout
	case "candidate":
		fixture.catalog = `[{"id":"unconfirmed","label":"sbx · demo","target":"demo.sbx","session":"default","enabled":true}]`
	}
	return output, &exec.ExitError{}
}

func cancelOnFinalCreatedCheck(t *testing.T, fixture *lifecycleFixture, mode string, attempts, reads int) {
	t.Helper()
	if mode == "cancel during final check" && attempts == 2 && reads == 4 {
		require.NoError(t, store.SetEnabled(fixture.scope, "new", store.Disabled))
	}
}

func TestFailedEnableObservationDoesNotCancelRegistrationIntent(t *testing.T) {
	for _, baselineFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot first", true: "restart baseline first"}[baselineFirst], func(t *testing.T) { testFailedEnableObservation(t, baselineFirst) })
	}
}

func testFailedEnableObservation(t *testing.T, baselineFirst bool) {
	fixture, _, _ := newCreatedFixture(t)
	ready := delayCreatedProvisioning(fixture, "pending")
	fixture.scheduler().handle(createdSandboxEvent())
	*ready = true
	fixture.request = func(*http.Request) (*http.Response, error) {
		return lifecycleResponse(200, `[{"uuid":"new","name":"demo","status":"running"}]`), nil
	}
	original := fixture.run
	attempts, unreadable := 0, true
	fixture.run = func(ctx context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		if strings.HasPrefix(call, "herdr machine add ") {
			attempts++
			return "remote platform detection failed. Machine was not saved.", &exec.ExitError{}
		}
		if attempts > 0 && unreadable && call == "herdr machine list --json" {
			return "", fmt.Errorf("connection inventory temporarily unreadable")
		}
		return original(ctx, command)
	}
	require.Error(t, fixture.service.registrations.SetEnabled(context.Background(), fixture.scope, "new", store.Enabled))
	unreadable = false
	before := attempts
	if baselineFirst {
		restartLifecycleService(fixture)
		require.NoError(t, fixture.service.registrations.CaptureBaseline(context.Background(), "/tmp/lifecycle-test.sock"))
	}
	rows, err := fixture.service.registrations.SandboxRows(context.Background(), "/tmp/lifecycle-test.sock")
	require.NoError(t, err)
	require.Equal(t, store.Enabled, rows[0].Enabled, "observed absence must not become an explicit cancellation")
	restartLifecycleService(fixture)
	fixture.scheduler().processCatchup(true)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Contains(t, saved.CreationRequests, store.StatusKey(fixture.scope, "new"))
	state, err := store.LoadState()
	require.NoError(t, err)
	require.True(t, state.Scopes[fixture.scope][0].Pending, "an unreadable add result must remain inert")
	require.Equal(t, before, attempts, "positive intent must not replay the uncertain add")
}
