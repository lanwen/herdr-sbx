package registration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// SandboxRow is a passive inventory/catalog observation for the manager.
type SandboxRow struct {
	Sandbox          sbx.Sandbox
	Scope            string
	Management       store.ManagementChoice
	Owned            bool
	Label, ProfileID string
	Matches          []herdr.Machine
	Status           store.SandboxStatus
}

// SandboxRows observes inventory and registrations without connecting to guests.
// New identities become watched unless an existing registration requires adoption.
func (s *Service) SandboxRows(ctx context.Context, socket string) ([]SandboxRow, error) {
	if socket == "" {
		return nil, errors.New("sandbox daemon socket is unavailable")
	}
	scope := store.ScopeID(socket)
	sandboxes, err := s.inventory(ctx, socket)
	if err != nil {
		_ = store.MarkScopeStale(scope, err.Error())
		return nil, err
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return nil, fmt.Errorf("read Herdr catalog: %s", commandError(err, output))
	}
	state, err := store.LoadState()
	if err != nil {
		return nil, err
	}
	return observeManagement(scope, sandboxes, machines, state)
}

func observeManagement(scope string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State) ([]SandboxRow, error) {
	var rows []SandboxRow
	err := store.UpdateStatusStore(func(status *store.StatusStore) {
		present := map[string]bool{}
		for _, sb := range sandboxes {
			present[sb.ID] = true
		}
		for key, cached := range status.Sandboxes {
			if cached.Scope == scope && !present[cached.SandboxID] {
				cached.Stale = true
				cached.Detail = "not present in latest inventory; deletion is unconfirmed"
				status.Sandboxes[key] = cached
			}
		}
		for _, sb := range sandboxes {
			if sb.ID == "" {
				continue
			}
			trackedOutstanding := false
			row := SandboxRow{Scope: scope, Sandbox: sb, Management: store.Untracked}
			for _, item := range state.Scopes[scope] {
				if item.SandboxID == sb.ID && (item.ProfileID != "" || item.Pending) {
					trackedOutstanding = true
					if m := ownedTrackedMachine(machines, item); m != nil {
						row.Owned = true
						row.Label = m.Label
						row.ProfileID = m.ID
					}
					break
				}
			}
			for _, m := range machines {
				if m.Target == sb.Name+".sbx" || m.Label == sandboxMachineLabel(sb.Name) {
					row.Matches = append(row.Matches, m)
				}
			}
			key := store.StatusKey(scope, sb.ID)
			choice := status.Management[key]
			if !row.Owned && len(row.Matches) > 0 {
				row.Management = store.Unmanaged
				row.Label = row.Matches[0].Label
				row.ProfileID = row.Matches[0].ID
			} else {
				if choice == "" {
					choice = store.Watched
					status.Management[key] = choice
				}
				row.Management = choice
			}
			if choice == store.Disabled && (row.Owned || trackedOutstanding) {
				row.Management = store.Unwatched
			} else if choice == store.Unwatched || choice == store.Disabled {
				row.Management = choice
			}
			cached := status.Sandboxes[key]
			cached.Scope, cached.SandboxID, cached.Name, cached.Observed = scope, sb.ID, sb.Name, sb.Status
			cached.Management, cached.Owned, cached.Label, cached.ProfileID = row.Management, row.Owned, row.Label, row.ProfileID
			cached.ObservedAt = time.Now()
			cached.ConfirmPresent()
			status.SetSandboxDeletionFailure(key, "")
			status.Sandboxes[key] = cached
			row.Status = cached
			rows = append(rows, row)
		}
	})
	return rows, err
}

// SetManagement serializes lifecycle intent with automatic registration.
// Disable persists the opt-out before attempting ownership-safe removal.
func (s *Service) SetManagement(ctx context.Context, scope, id string, choice store.ManagementChoice) error {
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	var item *store.Tracked
	for i := range state.Scopes[scope] {
		if state.Scopes[scope][i].SandboxID == id {
			item = &state.Scopes[scope][i]
			break
		}
	}
	if choice == store.Watched && (item == nil || item.ProfileID == "") {
		cached, err := store.ReadStatusStore()
		if err != nil {
			return err
		}
		sb, ok := cached.Sandboxes[store.StatusKey(scope, id)]
		if !ok {
			return errors.New("refresh the sandbox inventory before watching")
		}
		machines, output, err := s.herdr.ListMachines(ctx)
		if err != nil {
			return fmt.Errorf("inspect Herdr catalog: %s", commandError(err, output))
		}
		for _, m := range machines {
			if m.Target == sb.Name+".sbx" || m.Label == sandboxMachineLabel(sb.Name) {
				return errors.New("approve adoption before watching this unmanaged registration")
			}
		}
	}
	if err := store.SetManagement(scope, id, choice); err != nil {
		return err
	}
	if choice != store.Disabled || item == nil {
		return nil
	}
	if item.Pending || item.ProfileID == "" {
		return errors.New("registration is unresolved; inspect the Herdr catalog before removing it")
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return fmt.Errorf("inspect Herdr catalog: %s", commandError(err, output))
	}
	if machineByID(machines, item.ProfileID) != nil {
		if ownedTrackedMachine(machines, *item) == nil {
			return errors.New("saved registration is no longer the owned Herdr profile")
		}
		output, err := s.herdr.RemoveMachine(ctx, item.ProfileID)
		if err != nil {
			return fmt.Errorf("remove owned registration: %s", commandError(err, output))
		}
		after, output, err := s.herdr.ListMachines(ctx)
		if err != nil {
			return fmt.Errorf("verify removal: %s", commandError(err, output))
		}
		if machineByID(after, item.ProfileID) != nil {
			return errors.New("Herdr registration removal is not confirmed")
		}
	}
	if err := removeTracked(&state, scope, id); err != nil {
		return err
	}
	if err := store.SaveState(state); err != nil {
		return err
	}
	return store.UpdateStatusStore(func(status *store.StatusStore) {
		key := store.StatusKey(scope, id)
		cached := status.Sandboxes[key]
		cached.Owned = false
		cached.Label = ""
		cached.ProfileID = ""
		cached.Management = store.Disabled
		status.Sandboxes[key] = cached
	})
}

func waitManagementLock(ctx context.Context) (func(), error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for the current reconciliation to finish: %w", err)
		}
		unlock, err := store.LockState()
		if err == nil || !errors.Is(err, store.ErrStateLocked) {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for the current reconciliation to finish: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Rename changes only a verified owned Herdr label, journaling uncertain results.
func (s *Service) Rename(ctx context.Context, scope, id, label string) error {
	if strings.TrimSpace(label) == "" || strings.ContainsFunc(label, unicode.IsControl) {
		return errors.New("enter a nonempty Herdr label without control characters")
	}
	unlock, err := store.LockState()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	for i := range state.Scopes[scope] {
		item := &state.Scopes[scope][i]
		if item.SandboxID != id {
			continue
		}
		machines, output, err := s.herdr.ListMachines(ctx)
		if err != nil {
			return fmt.Errorf("inspect Herdr catalog: %s", commandError(err, output))
		}
		owned := ownedTrackedMachine(machines, *item)
		if owned == nil {
			return errors.New("adopt or refresh the registration before renaming")
		}
		// Settle an acknowledged catalog observation before replacing rename intent.
		// The last request may have succeeded despite a lost response.
		item.Label = owned.Label
		item.PendingLabel = ""
		for _, m := range machines {
			if m.ID != item.ProfileID && m.Label == label {
				return errors.New("another Herdr registration already uses this label")
			}
		}
		item.CustomLabel = true
		item.PendingLabel = label
		if err := store.SaveState(state); err != nil {
			return err
		}
		sb := sbx.Sandbox{ID: id, Name: SandboxName(*item)}
		if err := s.recoverMachineLabel(ctx, scope, &state, machines, sb, item, func() bool { return ctx.Err() == nil }); err != nil {
			return err
		}
		return store.UpdateStatusStore(func(status *store.StatusStore) {
			key := store.StatusKey(scope, id)
			cached := status.Sandboxes[key]
			cached.Label = label
			status.Sandboxes[key] = cached
		})
	}
	return errors.New("registration is no longer available")
}
