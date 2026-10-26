package store

import "errors"

// EnabledChoice is the durable per-sandbox connection intent.
type EnabledChoice string

const (
	// Enabled permits setup and connection while the sandbox runs.
	Enabled EnabledChoice = "enabled"
	// Disabled prevents guest work and preserves the opt-out without an entry.
	Disabled EnabledChoice = "disabled"
)

// ReadEnabled reads choices discovered under the current state model.
func ReadEnabled() (map[string]EnabledChoice, error) {
	status, err := ReadStatusStore()
	return status.EnabledChoices, err
}

// SetEnabled saves a choice without translating old management choices.
func SetEnabled(scope, id string, choice EnabledChoice) error {
	_, err := AcceptEnabledChoice(scope, id, choice)
	return err
}

// AcceptEnabledChoice orders explicit commands before they wait for registration.
func AcceptEnabledChoice(scope, id string, choice EnabledChoice) (uint64, error) {
	revision, _, _, err := BeginEnabledChange(scope, id, choice)
	return revision, err
}

// BeginEnabledChange accepts a choice and atomically retains the last confirmed
// state for cancellation or an unverified result.
func BeginEnabledChange(scope, id string, choice EnabledChoice) (revision uint64, previous EnabledChoice, pending bool, err error) {
	if scope == "" || id == "" || (choice != Enabled && choice != Disabled) {
		return 0, "", false, errors.New("invalid sandbox enabled choice")
	}
	err = UpdateStatusStore(func(s *StatusStore) {
		key := StatusKey(scope, id)
		previous = s.confirmedEnabledChoice(key)
		pending = s.EnabledObservationPending[key]
		s.EnabledChoices[key] = choice
		delete(s.EnabledObservationPending, key)
		s.EnabledRevisions[key]++
		revision = s.EnabledRevisions[key]
	})
	return
}

// WorkerControl records whether first activation occurred and explicit Stop.
type WorkerControl struct {
	Generation  uint64 `json:"generation"`
	Initialized bool   `json:"initialized"`
	Stopped     bool   `json:"stopped"`
}

// SetWorkerControl atomically records the user's worker startup preference.
func SetWorkerControl(stopped bool) error {
	return UpdateStatusStore(func(s *StatusStore) {
		generation := uint64(1)
		if s.WorkerControl != nil {
			generation = s.WorkerControl.Generation + 1
		}
		s.WorkerControl = &WorkerControl{Generation: generation, Initialized: true, Stopped: stopped}
	})
}

// SetWorkerControlIfGeneration applies a settled command only if no newer choice exists.
func SetWorkerControlIfGeneration(stopped bool, generation uint64) (bool, error) {
	applied := false
	err := UpdateStatusStore(func(s *StatusStore) {
		current := uint64(0)
		if s.WorkerControl != nil {
			current = s.WorkerControl.Generation
		}
		if current != generation {
			return
		}
		s.WorkerControl = &WorkerControl{Initialized: true, Stopped: stopped, Generation: current + 1}
		applied = true
	})
	return applied, err
}

// SetEnabledIfRevision applies a baseline only when no newer explicit choice exists.
func SetEnabledIfRevision(scope, id string, choice EnabledChoice, revision uint64) (bool, error) {
	if scope == "" || id == "" || (choice != Enabled && choice != Disabled) {
		return false, errors.New("invalid sandbox enabled choice")
	}
	applied := false
	err := UpdateStatusStore(func(s *StatusStore) {
		key := StatusKey(scope, id)
		if s.EnabledRevisions[key] != revision {
			return
		}
		s.EnabledChoices[key] = choice
		delete(s.EnabledObservationPending, key)
		s.EnabledRevisions[key]++
		applied = true
	})
	return applied, err
}

// SettleEnabledChoice records the actual or last confirmed state after a command.
// Pending observations pause automatic work until a fresh catalog verifies it.
func SettleEnabledChoice(scope, id string, choice EnabledChoice, revision uint64, pending bool) (bool, error) {
	if scope == "" || id == "" || (choice != Enabled && choice != Disabled) {
		return false, errors.New("invalid sandbox enabled choice")
	}
	applied := false
	err := UpdateStatusStore(func(s *StatusStore) {
		key := StatusKey(scope, id)
		if s.EnabledRevisions[key] != revision {
			return
		}
		s.EnabledChoices[key] = choice
		s.EnabledRevisions[key]++
		delete(s.EnabledObservationPending, key)
		cached := s.Sandboxes[key]
		cached.Enabled = choice
		if pending {
			s.EnabledObservationPending[key] = true
			cached.Stale = true
			cached.Connection = "observation unavailable"
			cached.Detail = "The herdr connection change could not be verified. Automatic work waits for a fresh observation."
		}
		s.Sandboxes[key] = cached
		applied = true
	})
	return applied, err
}

func (s *StatusStore) confirmedEnabledChoice(key string) EnabledChoice {
	var previous EnabledChoice
	previous = s.EnabledChoices[key]
	if previous == "" {
		previous = Disabled
	}
	if cached := s.Sandboxes[key]; cached.EntryPresent && cached.NativeEnabled != nil {
		previous = Disabled
		if *cached.NativeEnabled {
			previous = Enabled
		}
	}
	return previous
}
