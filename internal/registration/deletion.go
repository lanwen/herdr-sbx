package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// DeletionResult describes an identity-checked cleanup attempt. Retry indicates
// that the lifecycle scheduler should retain the delete event for another try.
type DeletionResult struct {
	Retry  bool
	Detail string
}

// RemoveDeletedSandbox removes a profile only after the daemon confirms that
// the sandbox name no longer refers to its recorded identity.
func (s *Service) RemoveDeletedSandbox(ctx context.Context, scope, socket, sandboxID string) (DeletionResult, error) {
	if sandboxID == "" {
		return DeletionResult{}, nil
	}
	unlock, err := store.LockState()
	if err != nil {
		return DeletionResult{Retry: true}, err
	}
	defer unlock()
	return s.removeDeletedSandbox(ctx, scope, socket, sandboxID)
}

// removeDeletedSandbox requires the caller to hold the state lock.
func (s *Service) removeDeletedSandbox(ctx context.Context, scope, socket, sandboxID string) (DeletionResult, error) {
	if err := CheckAutomaticWork(ctx); err != nil {
		return DeletionResult{Retry: true}, err
	}
	if scope == "" || socket == "" || store.ScopeID(socket) != scope {
		return DeletionResult{Detail: "Lifecycle deletion retained state: the sandbox daemon scope does not match its saved association."}, nil
	}
	st, err := store.LoadState()
	if err != nil {
		return DeletionResult{Retry: true}, err
	}
	var item *store.Tracked
	for i := range st.Scopes[scope] {
		if st.Scopes[scope][i].SandboxID == sandboxID {
			item = &st.Scopes[scope][i]
			break
		}
	}
	if item == nil {
		return DeletionResult{}, nil
	}
	name := SandboxName(*item)
	if item.Pending || name == "" {
		return DeletionResult{Detail: "Lifecycle deletion retained an unresolved registration intent; inspect the Herdr machine catalog before retrying."}, nil
	}
	absent, err := s.sbx.SandboxAbsent(ctx, socket, name, item.SandboxID)
	// A confirmed removed entry retains guest metadata, not a native resource.
	// A verified name replacement can release that old identity locally, but must
	// never authorize removal of a machine now connecting to the reused name.
	if item.ProfileID == "" && (err == nil && absent || errors.Is(err, sbx.ErrSandboxIdentityChanged)) {
		if err := CheckAutomaticWork(ctx); err != nil {
			return DeletionResult{Retry: true}, err
		}
		return s.clearDeleted(&st, scope, sandboxID, name, "cleared its retained guest state")
	}
	if err != nil {
		return DeletionResult{Retry: true}, fmt.Errorf("sandbox absence is not confirmed: %w", err)
	}
	if !absent {
		return DeletionResult{Detail: "Lifecycle deletion retained state: sandbox absence is not confirmed."}, nil
	}

	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return DeletionResult{Retry: true}, fmt.Errorf("cannot inspect Herdr machines: %s", commandError(err, output))
	}
	_, issue := associatedMachine(scope, sbx.Sandbox{ID: item.SandboxID, Name: name}, machines, st)
	if issue != "" {
		detail := "Lifecycle deletion retained state: " + issue
		if err := store.SetRegistrationError(store.CodeOwnershipConflict, store.StatusKey(scope, sandboxID), detail); err != nil {
			return DeletionResult{Retry: true}, err
		}
		return DeletionResult{Detail: detail}, nil
	}
	if machineByID(machines, item.ProfileID) == nil {
		return s.clearDeleted(&st, scope, sandboxID, name, "machine was already absent")
	}
	if associatedTrackedMachine(machines, *item) == nil {
		return DeletionResult{Detail: "Lifecycle deletion retained state: saved machine is no longer the associated entry."}, nil
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return DeletionResult{Retry: true}, err
	}
	removed, err := s.herdr.RemoveMachine(ctx, item.ProfileID)
	after, _, verifyErr := s.herdr.ListMachines(ctx)
	if verifyErr != nil {
		return DeletionResult{Retry: true}, fmt.Errorf("entry removal outcome is unknown; saved association was kept: %w", verifyErr)
	}
	if machineByID(after, item.ProfileID) != nil {
		if err != nil {
			return DeletionResult{Retry: true}, fmt.Errorf("remove associated herdr entry: %s", commandError(err, removed))
		}
		return DeletionResult{Retry: true}, fmt.Errorf("herdr entry removal is not confirmed")
	}
	return s.clearDeleted(&st, scope, sandboxID, name, "removed its associated herdr entry")
}

func (s *Service) clearDeleted(st *store.State, scope, sandboxID, name, detail string) (DeletionResult, error) {
	// Clear the display cache first so a failed write keeps the association
	// record available for a later cleanup retry.
	if err := store.UpdateStatusStore(func(status *store.StatusStore) {
		key := store.StatusKey(scope, sandboxID)
		status.SetSandboxDeletionFailure(key, "")
		for errorKey, entry := range status.Errors {
			if entry.Scope == key && entry.Code == store.CodeOwnershipConflict {
				delete(status.Errors, errorKey)
			}
		}
		delete(status.Sandboxes, key)
		delete(status.EnabledObservationPending, key)
		delete(status.Preparation, key)
		delete(status.PreparationErrors, key)
		for attempt := range status.GuestPluginAttempts {
			if strings.HasPrefix(attempt, key+"\x00") {
				delete(status.GuestPluginAttempts, attempt)
			}
		}
	}); err != nil {
		return DeletionResult{Retry: true}, err
	}
	if err := removeTracked(st, scope, sandboxID); err != nil {
		return DeletionResult{Retry: true}, err
	}
	if err := store.SaveState(*st); err != nil {
		return DeletionResult{Retry: true}, err
	}
	return DeletionResult{Detail: fmt.Sprintf("%s deleted; %s.", name, detail)}, nil
}

// recoverDeletedSandboxes also catches deletions missed while the watcher was
// stopped. Inventory omissions alone are not proof: removal rechecks identity
// with the daemon and validates the saved Herdr profile under the state lock.
func (s *Service) recoverDeletedSandboxes(ctx context.Context, out io.Writer, scope, socket string) error {
	sandboxes, err := s.inventory(ctx, socket)
	if err != nil {
		return fmt.Errorf("list sandboxes for deletion recovery: %w", err)
	}
	present := make(map[string]bool, len(sandboxes))
	for _, sandbox := range sandboxes {
		present[sandbox.ID] = true
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	for _, item := range st.Scopes[scope] {
		if err := ctx.Err(); err != nil {
			return err
		}
		if present[item.SandboxID] || item.Pending || SandboxName(item) == "" {
			continue
		}
		result, err := s.removeDeletedSandbox(ctx, scope, socket, item.SandboxID)
		if err != nil {
			fmt.Fprintln(out, "Sandbox deletion deferred:", err)
		} else if result.Detail != "" {
			fmt.Fprintln(out, result.Detail)
		}
	}
	return nil
}
