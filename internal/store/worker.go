package store

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// WorkerStatus describes the detached worker, independently of guest readiness.
// LastSync records a completed lifecycle snapshot or successful catch-up.
type WorkerStatus struct {
	State    string    `json:"state"`
	Detail   string    `json:"detail,omitempty"`
	LastSync time.Time `json:"last_sync,omitempty"`
}

// RecordWorkerStatus updates worker telemetry. An empty state updates only the
// sync time, so a catch-up finishing after a disconnect cannot hide reconnecting.
func RecordWorkerStatus(state, detail string, synced bool) error {
	return UpdateStatusStore(func(s *StatusStore) {
		if s.Worker == nil {
			s.Worker = &WorkerStatus{}
		}
		if state != "" {
			s.Worker.State, s.Worker.Detail = state, detail
		}
		if synced {
			s.Worker.LastSync = time.Now()
		}
	})
}

// ReadWorkerStatus probes the existing lock without creating files or starting
// a worker. The OS releases the lock on exit, including crashes. An active
// daemon without telemetry is reported as unknown until it reports status.
func ReadWorkerStatus() (WorkerStatus, error) {
	path, err := StatePath()
	if err != nil {
		return WorkerStatus{}, err
	}
	file, err := os.OpenFile(path+".worker", os.O_RDONLY, 0)
	active := false
	if err == nil {
		defer file.Close()
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
				return WorkerStatus{}, err
			}
		} else if errors.Is(err, syscall.EWOULDBLOCK) {
			active = true
		} else {
			return WorkerStatus{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return WorkerStatus{}, err
	}
	saved, err := ReadStatusStore()
	if err != nil {
		return WorkerStatus{}, err
	}
	status := WorkerStatus{State: "unknown", Detail: "Daemon is active but has not reported status yet."}
	if saved.Worker != nil {
		status.LastSync = saved.Worker.LastSync
		if saved.Worker.State != "stopped" {
			status = *saved.Worker
		}
	}
	if !active {
		status.State, status.Detail = "stopped", "Daemon is not running."
	}
	return status, nil
}
