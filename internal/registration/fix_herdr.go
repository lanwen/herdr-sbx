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
func (s *Service) FixGuestHerdr(ctx context.Context, out io.Writer, selection SandboxSelection, expectedHostVersion string) error {
	scope, socket, id, name := selection.Scope, selection.Socket, selection.ID, selection.Name
	ctx = WithExplicitAction(ctx)
	if id == "" || name == "" || scope == "" || socket == "" {
		return errors.New("refresh the sandbox inventory before fixing herdr")
	}
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	err = s.fixOwnedGuestHerdr(ctx, out, SandboxSelection{Scope: scope, Socket: socket, ID: id, Name: name}, expectedHostVersion)
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

func (s *Service) fixOwnedGuestHerdr(ctx context.Context, out io.Writer, selection SandboxSelection, expectedHostVersion string) error {
	scope, socket, id, name := selection.Scope, selection.Socket, selection.ID, selection.Name
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
	tracked := trackedSandbox(&state, scope, id)
	if !validFixAssociation(tracked, name) {
		return errors.New("refresh the registration before fixing herdr")
	}
	if err := s.checkFixMachine(ctx, *tracked); err != nil {
		return err
	}
	wanted, err := s.checkedHostVersion(ctx, expectedHostVersion)
	if err != nil {
		return err
	}

	sb := sbx.Sandbox{ID: id, Name: name, Status: "running"}
	return s.recoverGuestVersionTo(ctx, sandboxWork{Scope: scope, Sandbox: sb, Current: func() bool {
		return s.fixSandboxCurrent(ctx, socket, id, name) && sandboxWorkAllowed(ctx, scope, id)
	}}, func(message string) { fmt.Fprintln(out, message) }, false, wanted)
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

func (s *Service) checkedHostVersion(ctx context.Context, expectedHostVersion string) (string, error) {
	local, err := s.herdr.Version(ctx)
	if err != nil {
		return "", err
	}
	wanted := version.Parse(local)
	if wanted == "" || strings.TrimSpace(local) != "herdr "+wanted || version.Parse(expectedHostVersion) != wanted {
		return "", errors.New("host herdr version changed or is not an exact release; refresh before fixing herdr")
	}
	return wanted, nil
}

func validFixAssociation(tracked *store.Tracked, name string) bool {
	return tracked != nil && !tracked.Pending && tracked.ProfileID != "" && tracked.Target == name+".sbx"
}

func (s *Service) checkFixMachine(ctx context.Context, tracked store.Tracked) error {
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return fmt.Errorf("inspect herdr catalog: %s", commandError(err, output))
	}
	if associatedTrackedMachine(machines, tracked) == nil {
		return errors.New("saved registration is no longer the associated herdr entry")
	}
	return nil
}
