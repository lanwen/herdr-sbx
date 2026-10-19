package store

import (
	"errors"
	"time"
)

// GuestPluginAttempt records a transfer or build that began for one exact
// source revision. An empty detail means the outcome was not confirmed.
type GuestPluginAttempt struct {
	Failure   *PluginFailure `json:"failure,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	Detail    string         `json:"detail,omitempty"`
}

func guestPluginAttemptKey(scope, sandboxID, pluginID, revision string) string {
	return StatusKey(scope, sandboxID) + "\x00" + pluginID + "\x00" + revision
}

func validGuestPluginAttempt(scope, sandboxID, pluginID, revision string) bool {
	return scope != "" && sandboxID != "" && ValidPluginID(pluginID) && revision != ""
}

// GuestPluginAttemptFor reads the saved attempt for one sandbox, plugin, and
// exact source revision. A previous failed or interrupted attempt stays saved
// until guest availability is verified or Re-sync explicitly retries it.
func GuestPluginAttemptFor(scope, sandboxID, pluginID, revision string) (GuestPluginAttempt, bool, error) {
	if !validGuestPluginAttempt(scope, sandboxID, pluginID, revision) {
		return GuestPluginAttempt{}, false, errors.New("invalid guest plugin attempt identity")
	}
	status, err := ReadStatusStore()
	if err != nil {
		return GuestPluginAttempt{}, false, err
	}
	attempt, found := status.GuestPluginAttempts[guestPluginAttemptKey(scope, sandboxID, pluginID, revision)]
	return attempt, found, nil
}

// BeginGuestPluginAttempt atomically claims one transfer/build attempt. The
// explicit Re-sync action may replace an existing claim after inspecting the
// guest for a manually installed matching revision.
func BeginGuestPluginAttempt(scope, sandboxID, pluginID, revision string, explicit bool) (bool, error) {
	if !validGuestPluginAttempt(scope, sandboxID, pluginID, revision) {
		return false, errors.New("invalid guest plugin attempt identity")
	}
	allowed := false
	err := UpdateStatusStore(func(status *StatusStore) {
		key := guestPluginAttemptKey(scope, sandboxID, pluginID, revision)
		if _, exists := status.GuestPluginAttempts[key]; exists && !explicit {
			return
		}
		status.GuestPluginAttempts[key] = GuestPluginAttempt{StartedAt: time.Now()}
		allowed = true
	})
	return allowed, err
}

// FinishGuestPluginAttempt retains a failed attempt or clears one whose
// matching guest installation was verified. It never clears another revision.
func FinishGuestPluginAttempt(scope, sandboxID, pluginID, revision, detail string) error {
	if !validGuestPluginAttempt(scope, sandboxID, pluginID, revision) {
		return errors.New("invalid guest plugin attempt identity")
	}
	return UpdateStatusStore(func(status *StatusStore) {
		key := guestPluginAttemptKey(scope, sandboxID, pluginID, revision)
		if detail == "" {
			delete(status.GuestPluginAttempts, key)
			return
		}
		attempt := status.GuestPluginAttempts[key]
		if attempt.StartedAt.IsZero() {
			attempt.StartedAt = time.Now()
		}
		attempt.Detail = detail
		status.GuestPluginAttempts[key] = attempt
	})
}

// SaveGuestPluginAttemptFailure preserves structured recovery and the actual attempt time.
func SaveGuestPluginAttemptFailure(scope, sandboxID, pluginID, revision string, failure PluginFailure) error {
	if !validGuestPluginAttempt(scope, sandboxID, pluginID, revision) {
		return errors.New("invalid guest plugin attempt identity")
	}
	return UpdateStatusStore(func(status *StatusStore) {
		key := guestPluginAttemptKey(scope, sandboxID, pluginID, revision)
		attempt := status.GuestPluginAttempts[key]
		if attempt.StartedAt.IsZero() {
			attempt.StartedAt = time.Now()
		}
		failure.OccurredAt = attempt.StartedAt
		attempt.Failure = &failure
		attempt.Detail = failure.Detail
		if failure.Output != "" {
			attempt.Detail += ": " + failure.Output
		}
		status.GuestPluginAttempts[key] = attempt
	})
}
