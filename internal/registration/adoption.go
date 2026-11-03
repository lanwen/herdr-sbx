package registration

import (
	"context"
	"fmt"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func needsAssociation(state store.State, scope, id, profileID string) bool {
	for _, item := range state.Scopes[scope] {
		if item.SandboxID == id {
			return item.Pending || item.ProfileID != profileID
		}
	}
	return true
}

// Adoption is local bookkeeping, but still requires identity after the catalog read.
func (r *reconciliation) verifyAdoptions() error {
	for _, sb := range r.sandboxes {
		if err := CheckAutomaticWork(r.ctx); err != nil {
			return err
		}
		if err := r.service.verifyAdoption(r.ctx, r.prerequisites.Socket, sb, r.state, r.machines); err != nil {
			return fmt.Errorf("verify sandbox before adopting its Herdr connection: %w", err)
		}
	}
	return CheckAutomaticWork(r.ctx)
}

func (s *Service) verifyAdoption(ctx context.Context, socket string, sb sbx.Sandbox, state store.State, machines []herdr.Machine) error {
	machine, issue := associatedMachine(store.ScopeID(socket), sb, machines, state)
	if issue != "" || machine == nil || !needsAssociation(state, store.ScopeID(socket), sb.ID, machine.ID) {
		return nil
	}
	_, err := s.SandboxObservation(ctx, socket, sb)
	return err
}
