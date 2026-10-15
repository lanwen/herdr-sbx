package store

import "errors"

// ManagementChoice separates saved lifecycle intent from runtime state.
type ManagementChoice string

const (
	// Watched follows lifecycle events and synchronizes selected plugins.
	Watched ManagementChoice = "watched"
	// Unwatched retains ownership without changing the existing registration.
	Unwatched ManagementChoice = "unwatched"
	// Disabled stops management and requests removal of the owned registration.
	Disabled ManagementChoice = "disabled"
	// Untracked has no saved management choice or owned registration.
	Untracked ManagementChoice = "untracked"
	// Unmanaged has a matching registration that requires approved adoption.
	Unmanaged ManagementChoice = "unmanaged"
)

// WorkerControl records whether first activation occurred and explicit Stop.
type WorkerControl struct {
	Initialized bool `json:"initialized"`
	Stopped     bool `json:"stopped"`
}

// ReadManagement reads saved choices without observing any external service.
func ReadManagement() (map[string]ManagementChoice, error) {
	status, err := ReadStatusStore()
	return status.Management, err
}

// SetManagement saves a lifecycle choice. Services serialize this with registration.
func SetManagement(scope, id string, choice ManagementChoice) error {
	if scope == "" || id == "" || (choice != Watched && choice != Unwatched && choice != Disabled) {
		return errors.New("invalid sandbox management choice")
	}
	return UpdateStatusStore(func(s *StatusStore) { s.Management[StatusKey(scope, id)] = choice })
}

// SetWorkerControl atomically records the user's worker startup preference.
func SetWorkerControl(stopped bool) error {
	return UpdateStatusStore(func(s *StatusStore) { s.WorkerControl = &WorkerControl{Initialized: true, Stopped: stopped} })
}
