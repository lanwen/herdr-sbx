package registration

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// SandboxInventory returns the current daemon inventory for a known socket.
func (s *Service) SandboxInventory(ctx context.Context, socket string) ([]sbx.Sandbox, error) {
	return s.inventory(ctx, socket)
}

// SandboxRunning confirms that the expected identity is still running.
func (s *Service) SandboxRunning(socket string, expected sbx.Sandbox) bool {
	return s.sandboxRunning(socket, expected)
}

// ClearPreparationFailure clears one sandbox's retry cooldown.
func (s *Service) ClearPreparationFailure(scope, sandboxID string) error {
	return store.ClearPreparationFailure(store.StatusKey(scope, sandboxID))
}

// ReconcileSandbox registers one selected sandbox after verifying its identity
// before each externally visible registration step.
func (s *Service) ReconcileSandbox(ctx context.Context, out io.Writer, id, name string, stillCurrent func() bool) error {
	if id == "" || name == "" {
		return errors.New("sandbox has no stable identity")
	}
	var current func(sbx.Sandbox) bool
	if stillCurrent != nil {
		current = func(sbx.Sandbox) bool { return stillCurrent() }
	}
	return s.reconcileMatching(ctx, out, func(candidate sbx.Sandbox) bool {
		return candidate.ID == id && candidate.Name == name
	}, current)
}

// ReconcileAuthorized performs the explicitly approved temporary-access
// installation path for one selected sandbox.
func (s *Service) ReconcileAuthorized(ctx context.Context, out io.Writer, scope, socket, id, name string) error {
	if id == "" || name == "" {
		return errors.New("sandbox has no stable identity")
	}
	if err := store.ClearPreparationFailure(store.StatusKey(scope, id)); err != nil {
		return err
	}
	options := installationOptions{Scope: scope, Socket: socket, AuthorizedID: id}
	return s.reconcileWithInstallation(ctx, out, func(candidate sbx.Sandbox) bool {
		return candidate.ID == id && candidate.Name == name
	}, nil, options)
}

// RetryPreparation clears the failed-attempt cooldown after confirming that
// the named sandbox is still part of the current daemon inventory.
func (s *Service) RetryPreparation(ctx context.Context, out io.Writer, name string) error {
	if name == "" {
		return errors.New("enter a sandbox name")
	}
	p := s.Inspect()
	if p.Scope == "" || p.Socket == "" || !hasLevel(p.Checks, "Sandbox daemon", precheck.Ready) || hasBlocked(p.Checks, "Docker authorization") {
		return errors.New("retry needs a running sandbox daemon and readable sandbox inventory")
	}
	sandboxes, err := s.inventory(context.Background(), p.Socket)
	if err != nil {
		return err
	}
	for _, sb := range sandboxes {
		if sb.Name == name {
			return s.RetrySelected(ctx, out, p.Scope, p.Socket, sb.ID, sb.Name)
		}
	}
	return errors.New("sandbox is not in the current inventory")
}

// RetrySelected preserves the selected sandbox identity across cooldown reset
// and reconciliation, so a same-name replacement cannot receive the retry.
func (s *Service) RetrySelected(ctx context.Context, out io.Writer, scope, socket, id, name string) error {
	if id == "" || name == "" {
		return errors.New("sandbox has no stable identity")
	}
	if err := store.ClearPreparationFailure(store.StatusKey(scope, id)); err != nil {
		return errors.New("clear preparation retry state: " + err.Error())
	}
	return s.ReconcileSandbox(ctx, out, id, name, func() bool {
		items, err := s.inventory(ctx, socket)
		if err != nil {
			return false
		}
		for _, candidate := range items {
			if candidate.ID == id && candidate.Name == name && strings.EqualFold(candidate.Status, "running") {
				return true
			}
		}
		return false
	})
}

// RecoverPendingPolicy resumes cleanup from the status journal on activation.
func (s *Service) RecoverPendingPolicy(scope, socket string) error {
	return s.recoverPendingPolicy(scope, socket)
}

// InstallationAccess returns the exact HTTPS endpoints requested by the
// temporary installer, in display order.
func (s *Service) InstallationAccess() []sbx.PolicyAccess {
	return append([]sbx.PolicyAccess(nil), installerAccess[:]...)
}
