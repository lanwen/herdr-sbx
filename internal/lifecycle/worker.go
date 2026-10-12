package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/store"
)

// StartWorker starts a detached plugin worker and writes its output beside the
// registration journal.
func (s *Service) StartWorker() error {
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
	log, err := os.OpenFile(statePath+".worker.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(executable, "worker")
	command.Stdin = nil
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return command.Start()
}

// Worker supervises a lifecycle watch for as long as this plugin is enabled
// and the local Herdr server remains available across handoffs.
func (s *Service) Worker(out io.Writer) error {
	out = &lockedWriter{out: out}
	unlock, err := store.LockWorker()
	if err != nil {
		return errors.New("a Docker Sandboxes lifecycle worker is already active")
	}
	defer unlock()
	report := func(state, detail string, synced bool) {
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	livenessDone := make(chan struct{})
	go func() {
		defer close(livenessDone)
		s.stopWhenServerEnds(ctx, cancel)
	}()
	defer func() {
		cancel()
		<-livenessDone
	}()
	fmt.Fprintln(out, "Docker Sandboxes worker started. It stops when the local Herdr server stops.")
	for {
		err := s.watchWithStatus(ctx, out, report)
		if ctx.Err() != nil || !s.workerAlive() {
			return nil
		}
		if err != nil {
			report("reconnecting", err.Error(), false)
			fmt.Fprintln(out, "Docker Sandboxes worker reconnecting:", err)
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
