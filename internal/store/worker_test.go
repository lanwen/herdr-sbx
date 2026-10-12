package store

import (
	"testing"
)

func TestWorkerHealthUsesLockAndPreservesLastSync(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	status, err := ReadWorkerStatus()
	if err != nil || status.State != "stopped" {
		t.Fatalf("initial status: %#v %v", status, err)
	}
	unlock, err := LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	status, err = ReadWorkerStatus()
	if err != nil || status.State != "unknown" {
		t.Fatalf("legacy worker status: %#v %v", status, err)
	}
	if err := RecordWorkerStatus("running", "connected", true); err != nil {
		t.Fatal(err)
	}
	status, err = ReadWorkerStatus()
	if err != nil || status.State != "running" || status.LastSync.IsZero() {
		t.Fatalf("running status: %#v %v", status, err)
	}
	if err := RecordWorkerStatus("reconnecting", "stream lost", false); err != nil {
		t.Fatal(err)
	}
	retry, err := ReadWorkerStatus()
	if err != nil || retry.State != "reconnecting" || !retry.LastSync.Equal(status.LastSync) {
		t.Fatalf("retry status: %#v %v", retry, err)
	}
	if err := RecordWorkerStatus("", "", true); err != nil {
		t.Fatal(err)
	}
	retry, err = ReadWorkerStatus()
	if err != nil || retry.State != "reconnecting" || retry.Detail != "stream lost" {
		t.Fatalf("catch-up hid disconnect: %#v %v", retry, err)
	}
	// Simulate exit without a final status write. The remaining saved state must
	// never make an unlocked worker look alive.
	unlock()
	unlock = nil
	status, err = ReadWorkerStatus()
	if err != nil || status.State != "stopped" || status.LastSync.IsZero() {
		t.Fatalf("exited status: %#v %v", status, err)
	}
}
