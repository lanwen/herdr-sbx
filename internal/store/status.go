package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

// SandboxStatus stores the most recent observation and connection result.
type SandboxStatus struct {
	Scope       string                  `json:"scope"`
	Management  ManagementChoice        `json:"management,omitempty"`
	Owned       bool                    `json:"owned"`
	Label       string                  `json:"label,omitempty"`
	ProfileID   string                  `json:"profile_id,omitempty"`
	SandboxID   string                  `json:"sandbox_id"`
	Name        string                  `json:"name"`
	Observed    string                  `json:"observed"`
	Connection  string                  `json:"connection"`
	Detail      string                  `json:"detail"`
	Stale       bool                    `json:"stale,omitempty"`
	ObservedAt  time.Time               `json:"observed_at"`
	Updated     time.Time               `json:"updated"`
	GuestServer *GuestServerObservation `json:"guest_server,omitempty"`
}

// GuestServerObservation records the running server, never the executable version.
// A failed or stopped probe retains the last verified version with Current false.
type GuestServerObservation struct {
	Version    string    `json:"version,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	Current    bool      `json:"current"`
}

// RecordGuestServerObservation preserves the last verified version on failure.
func RecordGuestServerObservation(scope string, sandbox sbx.Sandbox, running bool, version string) error {
	return UpdateStatusStore(func(status *StatusStore) {
		key := StatusKey(scope, sandbox.ID)
		cached := status.Sandboxes[key]
		cached.Scope, cached.SandboxID, cached.Name = scope, sandbox.ID, sandbox.Name
		if cached.GuestServer == nil {
			cached.GuestServer = &GuestServerObservation{}
		}
		observed := cached.GuestServer
		observed.CheckedAt = time.Now()
		observed.Current = running && version != ""
		if observed.Current {
			observed.Version, observed.ObservedAt = version, observed.CheckedAt
		}
		status.Sandboxes[key] = cached
	})
}

// DeletionUnconfirmedDetail explains why an existing registration was retained.
const DeletionUnconfirmedDetail = "Cannot confirm whether this sandbox was deleted. Its herdr registration was kept."

// ConfirmPresent clears deletion uncertainty after the sandbox is observed again.
// Other connection and preparation explanations remain unchanged.
func (s *SandboxStatus) ConfirmPresent() {
	s.Stale = false
	if s.GuestServer != nil && !strings.EqualFold(s.Observed, "running") {
		s.GuestServer.Current = false
	}
	s.clearDeletionUncertainty()
}

func (s *SandboxStatus) clearDeletionUncertainty() {
	switch s.Detail {
	case DeletionUnconfirmedDetail, "sandbox deletion recovery is unavailable", "not present in latest inventory; deletion is unconfirmed":
		s.Detail = ""
	}
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

// StatusStore is the existing sidecar JSON document.
type StatusStore struct {
	Errors               map[string]ErrorRecord        `json:"errors,omitempty"`
	DismissedErrorGroups map[string]bool               `json:"dismissed_error_groups,omitempty"`
	Events               []EventRecord                 `json:"events,omitempty"`
	Management           map[string]ManagementChoice   `json:"management,omitempty"`
	WorkerControl        *WorkerControl                `json:"worker_control,omitempty"`
	Worker               *WorkerStatus                 `json:"worker,omitempty"`
	Sandboxes            map[string]SandboxStatus      `json:"sandboxes"`
	Preparation          map[string]time.Time          `json:"preparation"`
	PreparationErrors    map[string]string             `json:"preparation_errors,omitempty"`
	Warnings             map[string]string             `json:"warnings"`
	Installations        map[string]PolicyAttempt      `json:"installations,omitempty"`
	GuestPluginAttempts  map[string]GuestPluginAttempt `json:"guest_plugin_attempts,omitempty"`
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
	normalizeEvents(&store)
	normalizeErrorDismissals(&store)
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
	ensureErrorRefs(&store)
	change(&store)
	normalizeErrorDismissals(&store)
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
		status.ConfirmPresent()
		status.ObservedAt = time.Now()
		store.Sandboxes[key] = status
		store.SetSandboxDeletionFailure(key, "")
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
	if s.Errors == nil {
		s.Errors = map[string]ErrorRecord{}
	}
	if s.Management == nil {
		s.Management = map[string]ManagementChoice{}
	}
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
	if s.GuestPluginAttempts == nil {
		s.GuestPluginAttempts = map[string]GuestPluginAttempt{}
	}
}

func statusKey(scope, sandboxID string) string { return scope + "/" + sandboxID }

// StatusKey returns the storage key shared by observations and retries.
func StatusKey(scope, sandboxID string) string { return statusKey(scope, sandboxID) }
