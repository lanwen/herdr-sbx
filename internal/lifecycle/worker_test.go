package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestWorkerLogCompactionKeepsInheritedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.worker.log")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer log.Close()
	// Keep using the same inherited descriptor across multiple compactions,
	// just like a long-lived detached daemon's stdout and stderr.
	for _, marker := range []string{"first", "second", "third"} {
		command := exec.Command("sh", "-c", "cat; printf stderr-tail >&2")
		command.Stdin = strings.NewReader(strings.Repeat(marker, workerLogLimit/len(marker)+1))
		command.Stdout, command.Stderr = log, log
		require.NoError(t, command.Run())
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, compactWorkerLog(path))
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Len(t, after, workerLogRetained)
		require.Equal(t, before[len(before)-workerLogRetained:], after)
		require.True(t, bytes.HasSuffix(after, []byte("stderr-tail")), "inherited stderr output was lost")
	}
}

func TestWorkerLogCompactionHandlesMissingSmallAndUnreadableFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.worker.log")
	require.NoError(t, compactWorkerLog(path))
	{
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "missing log was created: %v", err)
	}
	require.NoError(t, os.WriteFile(path, []byte("recent output"), 0600))
	require.NoError(t, compactWorkerLog(path))
	{
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.EqualValues(t, "recent output", string(data))
	}
	badPath := path + ".bad"
	require.NoError(t, os.Symlink(badPath, badPath))
	{
		err := compactWorkerLog(badPath)
		require.ErrorIs(t, err, syscall.ELOOP, "unreadable log error = %v", err)
	}
}

func TestWorkerStartIsNoopWhileActive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	service := &Service{}
	var out bytes.Buffer
	require.NoError(t, service.StartWorker())
	require.NoError(t, service.Worker(&out))
	require.Equal(t, 0, out.Len(), "duplicate worker wrote %q", out.String())
	{
		_, err := os.Stat(filepath.Join(dir, "state.json.worker.log"))
		require.ErrorIs(t, err, os.ErrNotExist, "duplicate start created log: %v", err)
	}
	{
		_, err := store.LockWorker()
		require.ErrorIs(t, err, store.ErrWorkerActive, "original lock lost: %v", err)
	}
}

func TestWorkerStartReportsStorageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, nil, 0600))
	t.Setenv("HERDR_PLUGIN_STATE_DIR", path)
	service := &Service{}
	{
		err := service.StartWorker()
		require.Error(t, err, "start hid storage error")
	}
	{
		err := service.StopWorker(context.Background())
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrDaemonStopUnconfirmed)
	}
	var out bytes.Buffer
	{
		err := service.Worker(&out)
		require.Error(t, err, "worker hid storage error")
	}
}

func TestSavedStopBlocksAutomaticAndQueuedWorkerStarts(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	service := &Service{}
	require.NoError(t, service.StopWorker(context.Background()))
	require.NoError(t, service.StartWorker())
	var out bytes.Buffer
	require.NoError(t, service.Worker(&out))
	require.Equal(t, 0, out.Len(), "stopped worker started: %q", out.String())
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, status.WorkerControl)
	require.True(t, status.WorkerControl.Stopped)
}

func TestStopRequestKeepsAuthoritativeWorkerState(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	service := &Service{}
	{
		err := service.StopWorker(ctx)
		require.ErrorIs(t, err, ErrDaemonStopUnconfirmed)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Contains(t, err.Error(), store.CodeDaemonUnreachable)
		require.Contains(t, err.Error(), "Cannot confirm")
	}
	status, err := store.ReadWorkerStatus()
	require.NoError(t, err)
	require.NotEqual(t, "stopped", status.State, "reported stopped while lock still held")
	control, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, control.WorkerControl)
	require.True(t, control.WorkerControl.Stopped)
}

func TestExplicitStartWaitsForStoppingWorkerExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, store.SetWorkerControl(true))
	// The active daemon has already cancelled its context after seeing OFF.
	// Clearing OFF cannot revive that process; ON must wait for its lock to end.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	{
		err := prepareWorkerStart(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded, "ON did not wait for stopping daemon: %v", err)
	}
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.True(t, status.WorkerControl.Stopped)
	unlock()
	require.NoError(t, prepareWorkerStart(context.Background()))
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.False(t, status.WorkerControl.Stopped)
}

func TestStopWorkerWaitsForAuthoritativeExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, store.RecordWorkerStatus("running", "", false))
	service := &Service{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- service.StopWorker(ctx) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		saved, err := store.ReadStatusStore()
		require.NoError(t, err)
		if saved.WorkerControl != nil && saved.WorkerControl.Stopped {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Stop was not persisted")
		case <-ticker.C:
		}
	}
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned before daemon exit: %v", err)
	default:
	}
	// Exiting releases the OS lock even if the final telemetry write was lost.
	unlock()
	require.NoError(t, <-stopped)
	{
		err := service.StopWorker(ctx)
		require.NoError(t, err, "repeated stop failed: %v", err)
	}
	status, err := store.ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "stopped", status.State)
}

func TestStopWorkerStopsCooperativeWorker(t *testing.T) {
	f := newLifecycleFixture(t)
	entered := make(chan struct{}, 1)
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		switch cmd.Name + " " + strings.Join(cmd.Args, " ") {
		case "herdr plugin list --plugin lanwen.herdr-sbx --json":
			return `{"result":{"plugins":[{"plugin_id":"lanwen.herdr-sbx","enabled":true}]}}`, nil
		case "herdr status server --json":
			return `{"running":true}`, nil
		case "sbx daemon status --json":
			select {
			case entered <- struct{}{}:
			default:
			}
			return `{"status":"stopped","socket":"/tmp/lifecycle-test.sock"}`, nil
		}
		return f.defaultRun(ctx, cmd)
	}
	f.service.workerReconnect = time.Millisecond
	statePath, err := store.StatePath()
	require.NoError(t, err)
	logPath := statePath + ".worker.log"
	require.NoError(t, os.WriteFile(logPath, bytes.Repeat([]byte("x"), workerLogLimit+1), 0600))
	finished := make(chan error, 1)
	go func() { finished <- f.service.Worker(io.Discard) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer store.SetWorkerControl(true)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("daemon did not enter its watch loop")
	}
	logPoll := time.NewTicker(time.Millisecond)
	defer logPoll.Stop()
	for {
		info, err := os.Stat(logPath)
		require.NoError(t, err)
		if info.Size() == workerLogRetained {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("running daemon did not compact its log")
		case <-logPoll.C:
		}
	}
	require.NoError(t, f.service.StopWorker(ctx))
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("daemon still running after successful Stop")
	}
}

func TestStopWorkerReportsUnreadableConfirmation(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	path, err := store.StatePath()
	require.NoError(t, err)
	// A loop affects only the daemon lock read; saving Stop must still succeed.
	require.NoError(t, os.Symlink(path+".worker", path+".worker"))
	service := &Service{}
	err = service.StopWorker(context.Background())
	require.ErrorIs(t, err, ErrDaemonStopUnconfirmed)
	require.ErrorIs(t, err, syscall.ELOOP)
	require.Contains(t, err.Error(), store.CodeDaemonUnreachable)
	require.Contains(t, err.Error(), "Cannot confirm")
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, saved.WorkerControl)
	require.True(t, saved.WorkerControl.Stopped)
}

func TestOffBlocksEverySchedulerPath(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	require.NoError(t, store.SetWorkerControl(true))
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		t.Errorf("automatic command while OFF: %s %v", cmd.Name, cmd.Args)
		return "", errors.New("automatic command forbidden")
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		t.Errorf("automatic inventory/probe while OFF: %s", r.URL)
		return nil, errors.New("automatic request forbidden")
	}
	scheduler := f.scheduler()
	for _, action := range []string{"started", "created", "stopped", "deleted", "sync"} {
		scheduler.handle(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: action, SandboxID: "old", SandboxName: "demo"})
	}
	scheduler.processDeletes()
	scheduler.processPluginDefaults()
	scheduler.processCatchup(true)
	scheduler.recoverMissedDeletes()
	require.False(t, scheduler.removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}), "OFF reported deletion complete")
	require.NoError(t, f.service.watchContext(context.Background(), io.Discard))
	f.assertCount(1)
}

func TestStopReportsCancellingUntilWorkerSettles(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, store.RecordWorkerStatus("running", "following events", false))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	{
		err := (&Service{}).StopWorker(ctx)
		require.ErrorIs(t, err, ErrDaemonStopUnconfirmed, "unsettled worker reported stopped: %v", err)
	}
	status, err := store.ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "cancelling", status.State)
	require.EqualValues(t, "Cancelling", status.Detail)
	unlock()
	status, err = store.ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "stopped", status.State)
}

func TestUnreadableBaselineDefersAutomaticWork(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		if cmd.Name == "herdr" && strings.Join(cmd.Args, " ") == "machine list --json" {
			return "catalog offline", errors.New("unavailable")
		}
		return f.defaultRun(ctx, cmd)
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		t.Errorf("automatic work before fresh baseline: %s", r.URL)
		return nil, errors.New("forbidden")
	}
	err := f.service.watchContext(context.Background(), io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline")
	f.assertCount(1)
	require.Len(t, f.removed, 0, "removed registration without baseline: %v", f.removed)
}

func TestNewOffSupersedesPendingStart(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, store.SetWorkerControl(true))
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	generation := saved.WorkerControl.Generation
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	started := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { started <- prepareWorkerStartAtGeneration(ctx, generation) }()
	require.NoError(t, store.SetWorkerControl(true))
	unlock()
	{
		err := <-started
		require.ErrorIs(t, err, context.Canceled, "superseded ON was accepted: %v", err)
	}
	saved, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.True(t, saved.WorkerControl.Stopped)
	require.False(t, saved.WorkerControl.Generation != generation+1)
}

func TestStopConfirmationDoesNotInvalidateQueuedOn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, store.SetWorkerControl(true))
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	generation := saved.WorkerControl.Generation
	require.NoError(t, (&Service{}).WaitWorkerStopped(context.Background()))
	{
		err := prepareWorkerStartAtGeneration(context.Background(), generation)
		require.NoError(t, err, "queued ON rejected after stop confirmation: %v", err)
	}
	saved, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.False(t, saved.WorkerControl.Stopped)
	require.False(t, saved.WorkerControl.Generation != generation+1)
}

func TestNewScopeRequiresFreshBaseline(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	catalogReads := 0
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		if cmd.Name == "herdr" && strings.Join(cmd.Args, " ") == "machine list --json" {
			catalogReads++
			return "catalog unavailable", errors.New("unavailable")
		}
		return f.defaultRun(ctx, cmd)
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		t.Errorf("automatic work before new scope baseline: %s", r.URL)
		return nil, errors.New("forbidden")
	}
	baseline := managementBaseline{scope: "previous-scope", socket: "/tmp/previous-daemon.sock"}
	err := f.service.watchManaged(context.Background(), io.Discard, nil, &baseline)
	require.Error(t, err)
	require.Contains(t, err.Error(), "baseline")
	require.EqualValues(t, 1, catalogReads)
	require.EqualValues(t, "previous-scope", baseline.scope)
	require.Len(t, f.removed, 0)
}

func TestScopeChangeStopsWatchBeforeFurtherAutomaticWork(t *testing.T) {
	f := newLifecycleFixture(t)
	var changed atomic.Bool
	catalogReads := 0
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		switch cmd.Name + " " + strings.Join(cmd.Args, " ") {
		case "sbx daemon status --json":
			if changed.Load() {
				return `{"status":"running","socket":"/tmp/another-daemon.sock"}`, nil
			}
		case "herdr machine list --json":
			catalogReads++
		}
		return f.defaultRun(ctx, cmd)
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/events" {
			changed.Store(true)
			return lifecycleResponse(200, ""), nil
		}
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		t.Errorf("unexpected work during scope transition: %s", r.URL)
		return nil, errors.New("forbidden")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	baseline := managementBaseline{}
	err := f.service.watchManaged(ctx, io.Discard, nil, &baseline)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scope changed")
	require.EqualValues(t, 1, catalogReads)
	require.False(t, baseline.scope != f.scope)
	require.Len(t, f.removed, 0)
}
