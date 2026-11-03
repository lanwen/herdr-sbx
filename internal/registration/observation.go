package registration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// InventoryUnavailableError distinguishes an unreadable observation from a verified missing identity.
type InventoryUnavailableError struct{ Err error }

func (e *InventoryUnavailableError) Error() string {
	return "Sandbox inventory could not be read: " + e.Err.Error()
}
func (e *InventoryUnavailableError) Unwrap() error { return e.Err }

// PreAddUnavailableError permits a selected creation event to retry preparation.
// It is never emitted after a machine-add attempt has begun.
type PreAddUnavailableError struct{ Err error }

func (e *PreAddUnavailableError) Error() string { return e.Err.Error() }
func (e *PreAddUnavailableError) Unwrap() error { return e.Err }

// SandboxObservation verifies an exact identity, retrying only failed host reads.
func (s *Service) SandboxObservation(ctx context.Context, socket string, expected sbx.Sandbox) (sbx.Sandbox, error) {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return sbx.Sandbox{}, err
		}
		started := time.Now()
		sandboxes, err := s.inventory(ctx, socket)
		if err == nil {
			observed, identityErr := observedIdentity(sandboxes, expected)
			if identityErr != nil {
				s.logRegistration(ctx, registrationLog{Stage: "identity_check", Scope: store.ScopeID(socket), Sandbox: expected, Attempt: attempt, Started: started, Timeout: 5 * time.Second, Err: identityErr})
			}
			return observed, identityErr
		}
		last = err
		s.logRegistration(ctx, registrationLog{Stage: "inventory_check", Scope: store.ScopeID(socket), Sandbox: expected, Attempt: attempt, Started: started, Timeout: 5 * time.Second, Err: err})
		if attempt < 3 && !waitAddVerification(ctx) {
			return sbx.Sandbox{}, ctx.Err()
		}
	}
	return sbx.Sandbox{}, &InventoryUnavailableError{Err: last}
}

func observedIdentity(sandboxes []sbx.Sandbox, expected sbx.Sandbox) (sbx.Sandbox, error) {
	for _, observed := range sandboxes {
		if observed.ID == expected.ID && observed.Name == expected.Name {
			return observed, nil
		}
		if observed.Name == expected.Name {
			return sbx.Sandbox{}, fmt.Errorf("Sandbox %q now has a different identity: %w", expected.Name, sbx.ErrSandboxIdentityChanged)
		}
	}
	return sbx.Sandbox{}, fmt.Errorf("Sandbox %q (%s) is not present in the latest inventory", expected.Name, expected.ID)
}

func (s *Service) sandboxCurrent(ctx context.Context, socket string, expected sbx.Sandbox) error {
	observed, err := s.SandboxObservation(ctx, socket, expected)
	if err != nil {
		return err
	}
	if guestRuntimeAllowed(ctx, observed) || (createdSandboxID(ctx) == expected.ID && strings.EqualFold(observed.Status, "starting")) {
		return nil
	}
	return fmt.Errorf("Sandbox %q is %s; registration is waiting for it to run", expected.Name, observed.Status)
}

func inventoryPrerequisiteError(checks []precheck.Check, fallback error) error {
	for _, check := range checks {
		if check.Name == "Sandbox inventory" && check.Level != precheck.Ready {
			return &InventoryUnavailableError{Err: fmt.Errorf("%s", check.Detail)}
		}
	}
	return fallback
}
