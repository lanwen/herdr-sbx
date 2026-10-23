package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
)

// workerOutput adapts operation progress to daemon events without copying full
// command responses or multiline presentation reports into the log.
type workerOutput struct{ logger *slog.Logger }

func (w *workerOutput) Write(p []byte) (int, error) {
	if detail := workerLogDetail(string(p)); detail != "" {
		w.logger.Info("Daemon operation", slog.String("detail", detail))
	}
	return len(p), nil
}

func workerLogDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if first, _, multiline := strings.Cut(detail, "\n"); multiline {
		detail = first + " [additional lines omitted; see diagnostics]"
	}
	if len(detail) > 512 {
		detail = detail[:509] + "..."
	}
	return detail
}

// workerChecks records prerequisite transitions rather than repeating the panel
// report on every reconnect and reconciliation.
type workerChecks struct {
	mu       sync.Mutex
	logger   *slog.Logger
	previous map[string]precheck.Check
}

func (w *workerChecks) print(_ io.Writer, result precheck.Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	initial := w.previous == nil
	next := make(map[string]precheck.Check, len(result.Checks))
	ready := 0
	for _, check := range result.Checks {
		next[check.Name] = check
		if check.Level == precheck.Ready {
			ready++
		}
		previous, known := w.previous[check.Name]
		if known && previous.Level == check.Level && previous.Detail == check.Detail {
			continue
		}
		if initial && check.Level == precheck.Ready {
			continue
		}
		level := slog.LevelInfo
		if check.Level != precheck.Ready {
			level = slog.LevelWarn
		}
		w.logger.LogAttrs(context.Background(), level, "Prerequisite changed",
			slog.String("check", check.Name), slog.String("state", string(check.Level)), slog.String("detail", workerLogDetail(check.Detail)))
	}
	if initial {
		w.logger.Info("Prerequisites checked", slog.Int("ready", ready), slog.Int("total", len(result.Checks)))
	}
	w.previous = next
}

// rotateWorkerLog preserves the active inode so inherited stdout/stderr keep
// appending to it. Normal worker output is locked by the caller. As with any
// copy/truncate rotation, unsynchronized direct FD writes can race the final
// rewrite. Output can exceed the nominal limit between the 100 ms checks.
func rotateWorkerLog(path string) error {
	log, err := openWorkerLog(path, os.O_RDWR)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer log.Close()
	// The active inode is stable across rotation. Lock it so startup and worker
	// rotators in different processes cannot rewrite each other's snapshots.
	if err := syscall.Flock(int(log.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(log.Fd()), syscall.LOCK_UN)
	info, err := log.Stat()
	if err != nil || info.Size() <= workerLogLimit {
		return err
	}
	// Keep two complete chunks before the active remainder. A large burst drops
	// only older chunks, and reading never grows with an unbounded log file.
	chunks := (info.Size() - 1) / workerLogLimit
	remainderOffset := chunks * workerLogLimit
	if chunks == 1 {
		// Moving the previous archive preserves its complete chunk, including when
		// the active file has only just crossed the rotation threshold.
		previous, err := openWorkerLog(path+".1", os.O_RDONLY)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if previous != nil {
			if err := previous.Close(); err != nil {
				return err
			}
		}
		if err := os.Rename(path+".1", path+".2"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		chunk := make([]byte, workerLogLimit)
		if _, err := log.ReadAt(chunk, remainderOffset-2*workerLogLimit); err != nil {
			return err
		}
		if err := writeWorkerArchive(path+".2", chunk); err != nil {
			return err
		}
	}
	chunk := make([]byte, workerLogLimit)
	if _, err := log.ReadAt(chunk, remainderOffset-workerLogLimit); err != nil {
		return err
	}
	if err := writeWorkerArchive(path+".1", chunk); err != nil {
		return err
	}
	return retainWorkerLogTail(log, remainderOffset)
}

func retainWorkerLogTail(log *os.File, remainderOffset int64) error {
	// Read the retained bytes after staging archives, keeping the existing
	// direct-output read/rewrite race window short. Concurrent raw appends beyond
	// this snapshot remain subject to copy/truncate's limitation.
	current, err := log.Stat()
	if err != nil {
		return err
	}
	if current.Size() < remainderOffset {
		return fmt.Errorf("daemon log shrank during rotation: %d bytes before retained offset %d", current.Size(), remainderOffset)
	}
	if current.Size()-remainderOffset > workerLogLimit {
		// A concurrent raw writer crossed another boundary while archives were
		// staged. Leave the active data intact and rotate it on the next check.
		return nil
	}
	tail := make([]byte, current.Size()-remainderOffset)
	if _, err := log.ReadAt(tail, remainderOffset); err != nil {
		return err
	}
	if n, err := log.WriteAt(tail, 0); err != nil {
		return err
	} else if n != len(tail) {
		return io.ErrShortWrite
	}
	return log.Truncate(int64(len(tail)))
}

func writeWorkerArchive(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".worker-log-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// openWorkerLog rejects unsafe paths before changing permissions or contents.
// Nonblocking open lets regular-file validation reject FIFOs without waiting
// for another process to open their opposite end. It has no effect on files.
func openWorkerLog(path string, flags int) (*os.File, error) {
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("daemon log is not a regular file: %s", path)
	}
	if err == nil {
		err = file.Chmod(0600)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// workerResults logs domain outcomes by stable identity, so periodic scans do
// not repeat unchanged table rows and a recovered sandbox emits a fresh event.
type workerResults struct {
	mu       sync.Mutex
	logger   *slog.Logger
	previous map[string]registration.ResultObservation
}

func (w *workerResults) print(_ io.Writer, observation registration.ResultObservation) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.previous == nil {
		w.previous = make(map[string]registration.ResultObservation)
	}
	previous, known := w.previous[observation.SandboxID]
	if known && previous == observation {
		return
	}
	w.previous[observation.SandboxID] = observation
	level := slog.LevelInfo
	if observation.DiagnosticCode != "" {
		level = slog.LevelWarn
	}
	w.logger.LogAttrs(context.Background(), level, "Sandbox state changed",
		slog.String("sandbox_id", observation.SandboxID), slog.String("sandbox", observation.Name),
		slog.String("state", observation.Status), slog.String("code", observation.DiagnosticCode),
		slog.String("detail", workerLogDetail(observation.Detail)))
}
