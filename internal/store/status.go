package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

// SandboxStatus stores the most recent observation and connection result.
type SandboxStatus struct {
	Scope      string    `json:"scope"`
	SandboxID  string    `json:"sandbox_id"`
	Name       string    `json:"name"`
	Observed   string    `json:"observed"`
	Connection string    `json:"connection"`
	Detail     string    `json:"detail"`
	Stale      bool      `json:"stale,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Updated    time.Time `json:"updated"`
}

// OwnedPolicyRule binds cleanup to a daemon-created rule ID and its destination.
type OwnedPolicyRule struct {
	ID     string           `json:"id"`
	Access sbx.PolicyAccess `json:"access"`
}

// PolicyAttempt journals temporary access intent and confirmed owned rules.
type PolicyAttempt struct {
	PolicySandbox string            `json:"policy_sandbox,omitempty"`
	Scope         string            `json:"scope"`
	Sandbox       sbx.Sandbox       `json:"sandbox"`
	Owned         []OwnedPolicyRule `json:"owned,omitempty"`
	Intent        *sbx.PolicyAccess `json:"intent,omitempty"`
	Detail        string            `json:"detail,omitempty"`
}

// PolicySandboxName keeps the original selector for old journal records.
func (a PolicyAttempt) PolicySandboxName() string {
	if a.PolicySandbox != "" {
		return a.PolicySandbox
	}
	return a.Sandbox.ID
}

// StatusStore is the existing sidecar JSON document.
type StatusStore struct {
	Worker            *WorkerStatus            `json:"worker,omitempty"`
	Sandboxes         map[string]SandboxStatus `json:"sandboxes"`
	Preparation       map[string]time.Time     `json:"preparation"`
	PreparationErrors map[string]string        `json:"preparation_errors,omitempty"`
	Warnings          map[string]string        `json:"warnings"`
	Installations     map[string]PolicyAttempt `json:"installations,omitempty"`
}

// StatusSnapshot returns in-scope observations and pending temporary rules.
type StatusSnapshot struct {
	Sandboxes     []SandboxStatus
	Installations []PolicyAttempt
}

// ReadStatusStore reads status version fields without changing their schema.
func ReadStatusStore() (StatusStore, error) {
	path, err := statusPath()
	if err != nil {
		return StatusStore{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newStatusStore(), nil
	}
	if err != nil {
		return StatusStore{}, err
	}
	store := newStatusStore()
	if err := json.Unmarshal(data, &store); err != nil {
		return StatusStore{}, err
	}
	store.ensureMaps()
	return store, nil
}

// UpdateStatusStore serializes an update using the sidecar lock and atomic
// write protocol.
func UpdateStatusStore(change func(*StatusStore)) error {
	path, err := statusPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	store, err := ReadStatusStore()
	if err != nil {
		return err
	}
	change(&store)
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".status-*")
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
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Snapshot reads status for one scope in stable display order.
func Snapshot(scope string) (StatusSnapshot, error) {
	store, err := ReadStatusStore()
	if err != nil {
		return StatusSnapshot{}, err
	}
	var result StatusSnapshot
	for _, item := range store.Installations {
		if scope == "" || item.Scope == scope {
			result.Installations = append(result.Installations, item)
		}
	}
	for _, item := range store.Sandboxes {
		if scope == "" || item.Scope == scope {
			result.Sandboxes = append(result.Sandboxes, item)
		}
	}
	sort.Slice(result.Sandboxes, func(i, j int) bool { return result.Sandboxes[i].Name < result.Sandboxes[j].Name })
	return result, nil
}

// RecordSandboxStatus records a connection observation.
func RecordSandboxStatus(scope string, sandbox sbx.Sandbox, connection, detail string, stale bool) error {
	return UpdateStatusStore(func(store *StatusStore) {
		status := store.Sandboxes[statusKey(scope, sandbox.ID)]
		status.Scope, status.SandboxID, status.Name, status.Observed = scope, sandbox.ID, sandbox.Name, sandbox.Status
		status.Connection, status.Detail, status.Stale, status.Updated = connection, detail, stale, time.Now()
		store.Sandboxes[statusKey(scope, sandbox.ID)] = status
	})
}

// ObserveSandbox records a passive daemon observation without checking SSH.
func ObserveSandbox(scope string, sandbox sbx.Sandbox) error {
	return UpdateStatusStore(func(store *StatusStore) {
		key := statusKey(scope, sandbox.ID)
		status, found := store.Sandboxes[key]
		if !found {
			status = SandboxStatus{Scope: scope, SandboxID: sandbox.ID, Connection: "not checked", Detail: "awaiting registration"}
		}
		status.Name = sandbox.Name
		status.Observed = sandbox.Status
		status.Stale = false
		status.ObservedAt = time.Now()
		store.Sandboxes[key] = status
	})
}

// MarkScopeStale marks observations as stale after a failed refresh.
func MarkScopeStale(scope, detail string) error {
	return UpdateStatusStore(func(store *StatusStore) {
		for key, status := range store.Sandboxes {
			if status.Scope == scope {
				status.Stale = true
				status.Detail = detail
				store.Sandboxes[key] = status
			}
		}
	})
}

// PreparationAllowed reports whether the shared cooldown elapsed.
func PreparationAllowed(target string) (bool, error) {
	store, err := ReadStatusStore()
	if err != nil {
		return false, err
	}
	return !time.Now().Before(store.Preparation[target]), nil
}

// NotePreparationFailure records the retry cooldown and last error.
func NotePreparationFailure(target, detail string, retryDelay time.Duration) error {
	return UpdateStatusStore(func(store *StatusStore) {
		store.Preparation[target] = time.Now().Add(retryDelay)
		store.PreparationErrors[target] = detail
	})
}

// PreparationCooldownDetail returns the user-facing retry explanation.
func PreparationCooldownDetail(target string) (string, error) {
	store, err := ReadStatusStore()
	if err != nil {
		return "", err
	}
	detail := "Installation is on cooldown. Choose t to retry the host download and SSH transfer."
	if previous := store.PreparationErrors[target]; previous != "" {
		detail += " Last error: " + previous
	}
	return detail, nil
}

// ClearPreparationFailure clears the shared retry cooldown.
func ClearPreparationFailure(target string) error {
	return UpdateStatusStore(func(store *StatusStore) {
		delete(store.Preparation, target)
		delete(store.PreparationErrors, target)
	})
}

// WarningTransition updates one warning and reports whether it changed.
func WarningTransition(key, detail string) (bool, error) {
	changed := false
	if err := UpdateStatusStore(func(store *StatusStore) {
		if store.Warnings[key] != detail {
			store.Warnings[key] = detail
			changed = true
		}
	}); err != nil {
		return false, err
	}
	return changed, nil
}

// ClearWarning removes one saved warning.
func ClearWarning(key string) error {
	return UpdateStatusStore(func(store *StatusStore) { delete(store.Warnings, key) })
}

func statusPath() (string, error) {
	path, err := StatePath()
	if err != nil {
		return "", err
	}
	return path + ".status", nil
}

func newStatusStore() StatusStore {
	var store StatusStore
	store.ensureMaps()
	return store
}

func (s *StatusStore) ensureMaps() {
	if s.PreparationErrors == nil {
		s.PreparationErrors = map[string]string{}
	}
	if s.Installations == nil {
		s.Installations = map[string]PolicyAttempt{}
	}
	if s.Sandboxes == nil {
		s.Sandboxes = map[string]SandboxStatus{}
	}
	if s.Preparation == nil {
		s.Preparation = map[string]time.Time{}
	}
	if s.Warnings == nil {
		s.Warnings = map[string]string{}
	}
}

func statusKey(scope, sandboxID string) string { return scope + "/" + sandboxID }

// StatusKey returns the storage key shared by observations and retries.
func StatusKey(scope, sandboxID string) string { return statusKey(scope, sandboxID) }
