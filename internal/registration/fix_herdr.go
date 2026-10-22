package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

// FixGuestHerdr explicitly aligns an associated sandbox with the host release and
// then rechecks its setup. It never stops the guest server to force an update.
func (s *Service) FixGuestHerdr(ctx context.Context, out io.Writer, scope, socket, id, name, expectedHostVersion string) error {
	ctx = WithExplicitAction(ctx)
	if id == "" || name == "" || scope == "" || socket == "" {
		return errors.New("refresh the sandbox inventory before fixing herdr")
	}
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	err = s.fixOwnedGuestHerdr(ctx, out, scope, socket, id, name, expectedHostVersion)
	unlock()
	if err != nil {
		return err
	}
	// Reconciliation takes the same lock. Release it first, and verify the scope
	// again so a switched daemon cannot receive the selected sandbox's action.
	p := s.Inspect()
	if p.Scope != scope || p.Socket != socket {
		return errors.New("sandbox daemon changed; refresh before re-syncing")
	}
	return s.ReconcileSandbox(WithFreshGuestObservation(ctx), out, id, name, func() bool {
		return s.fixSandboxCurrent(ctx, socket, id, name) && sandboxWorkAllowed(ctx, scope, id)
	})
}

func (s *Service) fixOwnedGuestHerdr(ctx context.Context, out io.Writer, scope, socket, id, name, expectedHostVersion string) error {
	p := s.Inspect()
	if p.Scope != scope || p.Socket != socket || store.ScopeID(socket) != scope {
		return errors.New("sandbox daemon changed; refresh before fixing herdr")
	}
	if !s.fixSandboxCurrent(ctx, socket, id, name) {
		return errors.New("sandbox changed or stopped; refresh before fixing herdr")
	}
	status, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	if status.EnabledChoices[store.StatusKey(scope, id)] == store.Disabled {
		return errors.New("enable this sandbox before fixing herdr")
	}
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	var tracked *store.Tracked
	for i := range state.Scopes[scope] {
		item := &state.Scopes[scope][i]
		if item.SandboxID == id {
			tracked = item
			break
		}
	}
	if tracked == nil || tracked.Pending || tracked.ProfileID == "" || tracked.Target != name+".sbx" {
		return errors.New("refresh the registration before fixing herdr")
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return fmt.Errorf("inspect herdr catalog: %s", commandError(err, output))
	}
	if associatedTrackedMachine(machines, *tracked) == nil {
		return errors.New("saved registration is no longer the associated herdr entry")
	}
	local, err := s.herdr.Version(ctx)
	if err != nil {
		return err
	}
	wanted := version.Parse(local)
	if wanted == "" || strings.TrimSpace(local) != "herdr "+wanted || version.Parse(expectedHostVersion) != wanted {
		return errors.New("host herdr version changed or is not an exact release; refresh before fixing herdr")
	}
	sb := sbx.Sandbox{ID: id, Name: name, Status: "running"}
	return s.recoverGuestVersionTo(ctx, scope, sb, func() bool {
		return s.fixSandboxCurrent(ctx, socket, id, name) && sandboxWorkAllowed(ctx, scope, id)
	}, func(message string) { fmt.Fprintln(out, message) }, false, wanted)
}

func (s *Service) fixSandboxCurrent(ctx context.Context, socket, id, name string) bool {
	if ctx.Err() != nil {
		return false
	}
	items, err := s.inventory(ctx, socket)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.ID == id && item.Name == name && strings.EqualFold(item.Status, "running") {
			return true
		}
	}
	return false
}
