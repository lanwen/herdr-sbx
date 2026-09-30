package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Tracked records one owned or unresolved sandbox registration.
type Tracked struct {
	SandboxID               string `json:"sandbox_id"`
	Label                   string `json:"label"`
	PendingLabel            string `json:"pending_label,omitempty"`
	Target                  string `json:"target"`
	ProfileID               string `json:"profile_id,omitempty"`
	Pending                 bool   `json:"pending,omitempty"`
	ProjectWorkspaceDone    bool   `json:"project_workspace_done,omitempty"`
	ProjectWorkspacePending string `json:"project_workspace_pending,omitempty"`
}

// State is the version 1 registration ownership document.
type State struct {
	Version int                  `json:"version"`
	Scopes  map[string][]Tracked `json:"scopes"`
}

// LoadState reads version 1 state or returns an empty version 1 document.
func LoadState() (State, error) {
	path, err := StatePath()
	if err != nil {
		return State{}, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: 1, Scopes: map[string][]Tracked{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(contents, &state); err != nil {
		return State{}, fmt.Errorf("read plugin state: %w", err)
	}
	if state.Version != 1 || state.Scopes == nil {
		return State{}, errors.New("plugin state has an unsupported format")
	}
	return state, nil
}

// SaveState writes state atomically with mode 0600 and syncs the containing
// directory before returning.
func SaveState(state State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// StatePath returns the registration journal path.
func StatePath() (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(dir, "herdr-docker-sandboxes")
	}
	return filepath.Join(dir, "state.json"), nil
}

// LockState obtains the non-blocking registration lock.
func LockState() (func(), error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another reconciliation is already running")
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

// LockWorker obtains the non-blocking singleton worker lock beside state.json.
func LockWorker() (func(), error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".worker", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("worker lock is held")
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

// ScopeID hashes the daemon socket path used to isolate registration state.
func ScopeID(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])
}
