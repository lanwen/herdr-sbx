package registration

import (
	"context"
	"fmt"

	"github.com/lanwen/herdr-sbx/internal/store"
)

// DeletionResult describes an ownership-safe cleanup attempt. Retry indicates
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
	if item.ProfileID == "" || item.Pending || name == "" {
		return DeletionResult{Detail: "Lifecycle deletion retained an unresolved registration intent; inspect the Herdr machine catalog before retrying."}, nil
	}
	absent, err := s.sbx.SandboxAbsent(ctx, socket, name, item.SandboxID)
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
	if machineByID(machines, item.ProfileID) == nil {
		return s.clearDeleted(&st, scope, sandboxID, name, "machine was already absent")
	}
	if ownedTrackedMachine(machines, *item) == nil {
		return DeletionResult{Detail: "Lifecycle deletion retained state: saved machine is no longer the owned profile."}, nil
	}
	removed, err := s.herdr.RemoveMachine(ctx, item.ProfileID)
	if err != nil {
		return DeletionResult{Retry: true}, fmt.Errorf("remove owned Herdr machine: %s", commandError(err, removed))
	}
	return s.clearDeleted(&st, scope, sandboxID, name, "removed its owned Herdr machine")
}

func (s *Service) clearDeleted(st *store.State, scope, sandboxID, name, detail string) (DeletionResult, error) {
	if err := removeTracked(st, scope, sandboxID); err != nil {
		return DeletionResult{Retry: true}, err
	}
	if err := store.SaveState(*st); err != nil {
		return DeletionResult{Retry: true}, err
	}
	return DeletionResult{Detail: fmt.Sprintf("%s deleted; %s.", name, detail)}, nil
}
