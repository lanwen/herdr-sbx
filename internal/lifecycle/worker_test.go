package lifecycle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
)

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
	var out bytes.Buffer
	if err := service.Worker(&out); err == nil {
		t.Fatal("worker hid storage error")
	}
}
