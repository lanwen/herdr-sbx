package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/store"
)

// ErrDaemonStopUnconfirmed means Stop was saved but the daemon's exit could
// not be confirmed. Its wrapped cause distinguishes timeouts from read failures.
var ErrDaemonStopUnconfirmed = errors.New("Cannot confirm that the daemon stopped")

const workerLogLimit = 1 << 20 // Rotate daemon output after it exceeds 1 MiB.

// StartWorker starts a detached plugin worker and writes its output beside the
// registration journal. An already active worker is a successful no-op.
func (s *Service) StartWorker() error {
	stopped := false
	if err := store.UpdateStatusStore(func(status *store.StatusStore) {
		if status.WorkerControl == nil {
			status.WorkerControl = &store.WorkerControl{Initialized: true}
		}
		stopped = status.WorkerControl.Stopped
	}); err != nil {
		return err
	}
	if stopped {
		return nil
	}
	unlock, err := store.LockWorker()
	if errors.Is(err, store.ErrWorkerActive) {
		return nil
	}
	if err != nil {
		return err
	}
	// The child takes the authoritative lock. Concurrent starters may launch
	// children, but only one child can watch; the others exit successfully.
	unlock()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	statePath, err := store.StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return err
	}
	log, err := openWorkerLog(statePath+".worker.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer log.Close()
	if err := rotateWorkerLog(log.Name()); err != nil {
		return err
	}
	command := exec.Command(executable, "worker")
	command.Stdin = nil
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

// Worker supervises a lifecycle watch for as long as this plugin is enabled
// and the local Herdr server remains available across handoffs.
func (s *Service) Worker(out io.Writer) error {
	output := &lockedWriter{out: out}
	logger := slog.New(slog.NewTextHandler(output, nil))
	out = &workerOutput{logger: logger}
	unlock, err := store.LockWorker()
	if errors.Is(err, store.ErrWorkerActive) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	control, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	if control.WorkerControl != nil && control.WorkerControl.Stopped {
		return nil
	}
	var reportMu sync.Mutex
	lastState, lastDetail := "", ""
	report := func(state, detail string, synced bool) {
		reportMu.Lock()
		defer reportMu.Unlock()
		// Completion of an old operation must not overwrite OFF cancellation
		// with a late Running/reconnecting report.
		if state != "stopped" && !automaticAllowed(context.Background()) {
			state, detail, synced = "cancelling", "Cancelling", false
		}
		if state != lastState || detail != lastDetail {
			level := slog.LevelInfo
			if state == "reconnecting" {
				level = slog.LevelWarn
			}
			logger.LogAttrs(context.Background(), level, "Daemon state changed", slog.String("state", state), slog.String("detail", workerLogDetail(detail)))
			lastState, lastDetail = state, detail
		}
		if err := store.RecordWorkerStatus(state, detail, synced); err != nil {
			fmt.Fprintln(out, "Worker status was not saved:", err)
		}
	}
	report("starting", "Checking prerequisites and connecting to sandbox events.", false)
	defer report("stopped", "Worker exited.", false)
	s.herdr.RebindSocket(os.Getenv("HERDR_SOCKET_PATH"))
	defer s.herdr.RebindSocket("")
	if !s.workerAlive() {
		return errors.New("no participating local Herdr server is running")
	}
	// Worker owns this dedicated process; foreground commands keep their panel
	// printers and do not share these service instances.
	checks := &workerChecks{logger: logger}
	s.SetCheckPrinter(checks.print)
	s.registrations.SetCheckPrinter(checks.print)
	results := &workerResults{logger: logger}
	s.registrations.SetResultPrinter(results.print)
	logPath, err := store.StatePath()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		logErrorReported := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Rotate the inherited file, including direct stdout/stderr writes.
				// Output can exceed the cap briefly between checks.
				output.mu.Lock()
				logErr := rotateWorkerLog(logPath + ".worker.log")
				output.mu.Unlock()
				if logErr != nil {
					if !logErrorReported {
						fmt.Fprintln(out, "Daemon log could not be limited:", logErr)
					}
					logErrorReported = true
				} else {
					logErrorReported = false
				}
				status, err := store.ReadStatusStore()
				if err != nil || status.WorkerControl != nil && status.WorkerControl.Stopped {
					report("cancelling", "Cancelling", false)
					cancel()
					return
				}
			}
		}
	}()
	livenessDone := make(chan struct{})
	go func() {
		defer close(livenessDone)
		s.stopWhenServerEnds(ctx, cancel)
	}()
	defer func() {
		cancel()
		<-livenessDone
	}()
	logger.Info("Daemon started")
	baseline := managementBaseline{}
	for {
		err := s.watchManaged(ctx, out, report, &baseline)
		if !automaticAllowed(ctx) {
			return nil
		}
		if ctx.Err() != nil || !s.workerAlive() {
			return nil
		}
		if err != nil {
			report("reconnecting", err.Error(), false)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.workerReconnect):
		}
	}
}

func (s *Service) stopWhenServerEnds(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(s.workerPoll)
	defer ticker.Stop()
	var absentSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.herdr.PluginEnabled(ctx) {
				stop()
				return
			}
			if s.workerServerAlive(ctx) {
				absentSince = time.Time{}
				continue
			}
			if absentSince.IsZero() {
				absentSince = time.Now()
				continue
			}
			if time.Since(absentSince) >= s.serverHandoff {
				stop()
				return
			}
		}
	}
}

func (s *Service) workerAlive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.herdr.PluginEnabled(ctx) && s.workerServerAlive(ctx)
}

func (s *Service) workerServerAlive(ctx context.Context) bool {
	if s.herdr.LocalServerRunning(ctx) {
		return true
	}
	sessions, err := s.herdr.RunningSessionCount(ctx)
	if err != nil || sessions == 0 {
		return false
	}
	return s.herdr.RebindToRunningSession(ctx)
}

// StartWorkerExplicit waits for a stopping worker to exit before clearing Stop
// and starting its replacement. A cancelled worker cannot be resumed in place.
func (s *Service) StartWorkerExplicit() error {
	saved, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	var generation uint64
	if saved.WorkerControl != nil {
		generation = saved.WorkerControl.Generation
	}
	return s.StartWorkerExplicitAtGeneration(generation)
}

// StartWorkerExplicitAtGeneration binds ON to the choice accepted by the caller.
// A later OFF supersedes this command even while the old worker is settling.
func (s *Service) StartWorkerExplicitAtGeneration(generation uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := prepareWorkerStartAtGeneration(ctx, generation); err != nil {
		return err
	}
	return s.StartWorker()
}

func prepareWorkerStart(ctx context.Context) error {
	saved, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	var generation uint64
	if saved.WorkerControl != nil {
		generation = saved.WorkerControl.Generation
	}
	return prepareWorkerStartAtGeneration(ctx, generation)
}

func prepareWorkerStartAtGeneration(ctx context.Context, generation uint64) error {
	saved, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	if saved.WorkerControl != nil && saved.WorkerControl.Generation != generation {
		return context.Canceled
	}
	if saved.WorkerControl != nil && saved.WorkerControl.Stopped {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, err := store.ReadWorkerStatus()
			if err != nil {
				return err
			}
			if status.State == "stopped" {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting for previous daemon to stop: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	}
	accepted, err := store.SetWorkerControlIfGeneration(false, generation)
	if err != nil {
		return err
	}
	if !accepted {
		return context.Canceled
	}
	return nil
}

// StopWorker persists Stop and waits for the daemon's authoritative lock to
// release. A daemon that ignores Stop must not be reported as stopped.
func (s *Service) StopWorker(ctx context.Context) error {
	if err := store.SetWorkerControl(true); err != nil {
		return err
	}
	return s.WaitWorkerStopped(ctx)
}

// WaitWorkerStopped confirms an OFF choice already saved by the UI. It never
// issues another command, so an ON queued during cancellation keeps its identity.
func (s *Service) WaitWorkerStopped(ctx context.Context) error {
	if status, err := store.ReadWorkerStatus(); err == nil && status.State != "stopped" {
		if err := store.RecordWorkerStatus("cancelling", "Cancelling", false); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := store.ReadWorkerStatus()
		if err != nil {
			return fmt.Errorf("%s: %w because its status could not be read: %w", store.CodeDaemonUnreachable, ErrDaemonStopUnconfirmed, err)
		}
		if status.State == "stopped" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w. It may be busy or unreachable: %w", store.CodeDaemonUnreachable, ErrDaemonStopUnconfirmed, ctx.Err())
		case <-ticker.C:
		}
	}
}
