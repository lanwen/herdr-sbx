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
)

func TestWorkerLogCompactionKeepsInheritedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.worker.log")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	// Keep using the same inherited descriptor across multiple compactions,
	// just like a long-lived detached daemon's stdout and stderr.
	for _, marker := range []string{"first", "second", "third"} {
		command := exec.Command("sh", "-c", "cat; printf stderr-tail >&2")
		command.Stdin = strings.NewReader(strings.Repeat(marker, workerLogLimit/len(marker)+1))
		command.Stdout, command.Stderr = log, log
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compactWorkerLog(path); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != workerLogRetained || !bytes.Equal(after, before[len(before)-workerLogRetained:]) {
			t.Fatalf("compaction did not preserve the latest %d bytes: got %d", workerLogRetained, len(after))
		}
		if !bytes.HasSuffix(after, []byte("stderr-tail")) {
			t.Fatal("inherited stderr output was lost")
		}
	}
}

func TestWorkerLogCompactionHandlesMissingSmallAndUnreadableFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.worker.log")
	if err := compactWorkerLog(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing log was created: %v", err)
	}
	if err := os.WriteFile(path, []byte("recent output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := compactWorkerLog(path); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "recent output" {
		t.Fatalf("small log changed: %q, %v", data, err)
	}
	badPath := path + ".bad"
	if err := os.Symlink(badPath, badPath); err != nil {
		t.Fatal(err)
	}
	if err := compactWorkerLog(badPath); !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("unreadable log error = %v", err)
	}
}

func TestWorkerStartIsNoopWhileActive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	service := &Service{}
	var out bytes.Buffer
	if err := service.StartWorker(); err != nil {
		t.Fatal(err)
	}
	if err := service.Worker(&out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("duplicate worker wrote %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.worker.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate start created log: %v", err)
	}
	if _, err := store.LockWorker(); !errors.Is(err, store.ErrWorkerActive) {
		t.Fatalf("original lock lost: %v", err)
	}
}

func TestWorkerStartReportsStorageErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", path)
	service := &Service{}
	if err := service.StartWorker(); err == nil {
		t.Fatal("start hid storage error")
	}
	if err := service.StopWorker(context.Background()); err == nil || errors.Is(err, ErrDaemonStopUnconfirmed) {
		t.Fatalf("Stop write failure was misclassified: %v", err)
	}
	var out bytes.Buffer
	if err := service.Worker(&out); err == nil {
		t.Fatal("worker hid storage error")
	}
}

func TestSavedStopBlocksAutomaticAndQueuedWorkerStarts(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	service := &Service{}
	if err := service.StopWorker(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.StartWorker(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := service.Worker(&out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("stopped worker started: %q", out.String())
	}
	status, err := store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if status.WorkerControl == nil || !status.WorkerControl.Stopped {
		t.Fatal("automatic startup cleared Stop")
	}
}

func TestStopRequestKeepsAuthoritativeWorkerState(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	service := &Service{}
	if err := service.StopWorker(ctx); !errors.Is(err, ErrDaemonStopUnconfirmed) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), store.CodeDaemonUnreachable) || !strings.Contains(err.Error(), "Cannot confirm") {
		t.Fatalf("unresponsive daemon stop = %v, want coded confirmation timeout", err)
	}
	status, err := store.ReadWorkerStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.State == "stopped" {
		t.Fatal("reported stopped while lock still held")
	}
	control, err := store.ReadStatusStore()
	if err != nil || control.WorkerControl == nil || !control.WorkerControl.Stopped {
		t.Fatalf("Stop intent lost: %+v %v", control, err)
	}
}

func TestExplicitStartWaitsForStoppingWorkerExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	// The active daemon has already cancelled its context after seeing OFF.
	// Clearing OFF cannot revive that process; ON must wait for its lock to end.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := prepareWorkerStart(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ON did not wait for stopping daemon: %v", err)
	}
	status, err := store.ReadStatusStore()
	if err != nil || !status.WorkerControl.Stopped {
		t.Fatalf("ON cleared OFF before daemon exit: %+v %v", status, err)
	}
	unlock()
	if err := prepareWorkerStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err = store.ReadStatusStore()
	if err != nil || status.WorkerControl.Stopped {
		t.Fatalf("ON did not clear OFF after daemon exit: %+v %v", status, err)
	}
}

func TestStopWorkerWaitsForAuthoritativeExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := store.RecordWorkerStatus("running", "", false); err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- service.StopWorker(ctx) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		saved, err := store.ReadStatusStore()
		if err != nil {
			t.Fatal(err)
		}
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
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := service.StopWorker(ctx); err != nil {
		t.Fatalf("repeated stop failed: %v", err)
	}
	status, err := store.ReadWorkerStatus()
	if err != nil || status.State != "stopped" {
		t.Fatalf("status=%+v error=%v", status, err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	logPath := statePath + ".worker.log"
	if err := os.WriteFile(logPath, bytes.Repeat([]byte("x"), workerLogLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == workerLogRetained {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("running daemon did not compact its log")
		case <-logPoll.C:
		}
	}
	if err := f.service.StopWorker(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("daemon still running after successful Stop")
	}
}

func TestStopWorkerReportsUnreadableConfirmation(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	path, err := store.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	// A loop affects only the daemon lock read; saving Stop must still succeed.
	if err := os.Symlink(path+".worker", path+".worker"); err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	err = service.StopWorker(context.Background())
	if !errors.Is(err, ErrDaemonStopUnconfirmed) || !errors.Is(err, syscall.ELOOP) || !strings.Contains(err.Error(), store.CodeDaemonUnreachable) || !strings.Contains(err.Error(), "Cannot confirm") {
		t.Fatalf("unreadable confirmation = %v", err)
	}
	saved, err := store.ReadStatusStore()
	if err != nil || saved.WorkerControl == nil || !saved.WorkerControl.Stopped {
		t.Fatalf("Stop was lost: %+v %v", saved.WorkerControl, err)
	}
}

func TestOffBlocksEverySchedulerPath(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
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
	if scheduler.removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}) {
		t.Fatal("OFF reported deletion complete")
	}
	if err := f.service.watchContext(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	f.assertCount(1)
}

func TestStopReportsCancellingUntilWorkerSettles(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := store.RecordWorkerStatus("running", "following events", false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := (&Service{}).StopWorker(ctx); !errors.Is(err, ErrDaemonStopUnconfirmed) {
		t.Fatalf("unsettled worker reported stopped: %v", err)
	}
	status, err := store.ReadWorkerStatus()
	if err != nil || status.State != "cancelling" || status.Detail != "Cancelling" {
		t.Fatalf("cancellation status=%+v err=%v", status, err)
	}
	unlock()
	status, err = store.ReadWorkerStatus()
	if err != nil || status.State != "stopped" {
		t.Fatalf("settled status=%+v err=%v", status, err)
	}
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
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("baseline failure not reported: %v", err)
	}
	f.assertCount(1)
	if len(f.removed) != 0 {
		t.Fatalf("removed registration without baseline: %v", f.removed)
	}
}

func TestNewOffSupersedesPendingStart(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	generation := saved.WorkerControl.Generation
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	started := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { started <- prepareWorkerStartAtGeneration(ctx, generation) }()
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	unlock()
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("superseded ON was accepted: %v", err)
	}
	saved, err = store.ReadStatusStore()
	if err != nil || !saved.WorkerControl.Stopped || saved.WorkerControl.Generation != generation+1 {
		t.Fatalf("newer OFF was overwritten: %+v %v", saved.WorkerControl, err)
	}
}

func TestStopConfirmationDoesNotInvalidateQueuedOn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	generation := saved.WorkerControl.Generation
	if err := (&Service{}).WaitWorkerStopped(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := prepareWorkerStartAtGeneration(context.Background(), generation); err != nil {
		t.Fatalf("queued ON rejected after stop confirmation: %v", err)
	}
	saved, err = store.ReadStatusStore()
	if err != nil || saved.WorkerControl.Stopped || saved.WorkerControl.Generation != generation+1 {
		t.Fatalf("queued ON not accepted: %+v %v", saved.WorkerControl, err)
	}
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
	if err == nil || !strings.Contains(err.Error(), "baseline") || catalogReads != 1 {
		t.Fatalf("new scope reused old baseline: reads=%d err=%v", catalogReads, err)
	}
	if baseline.scope != "previous-scope" || len(f.removed) != 0 {
		t.Fatalf("unverified new scope was accepted: baseline=%+v removed=%v", baseline, f.removed)
	}
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
	if err == nil || !strings.Contains(err.Error(), "scope changed") {
		t.Fatalf("watch continued across scope change: %v", err)
	}
	if catalogReads != 1 || baseline.scope != f.scope || len(f.removed) != 0 {
		t.Fatalf("new scope mutated before old watch settled: reads=%d baseline=%+v removed=%v", catalogReads, baseline, f.removed)
	}
}
