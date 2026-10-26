package registration

import (
	"context"
	"errors"
	"io"

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

// RetrySelected preserves the selected sandbox identity across cooldown reset
// and reconciliation, so a same-name replacement cannot receive the retry.
func (s *Service) RetrySelected(ctx context.Context, out io.Writer, scope, socket, id, name string) error {
	if id == "" || name == "" {
		return errors.New("sandbox has no stable identity")
	}
	if err := store.ClearPreparationFailure(store.StatusKey(scope, id)); err != nil {
		return errors.New("clear preparation retry state: " + err.Error())
	}
	return s.ReconcileSandbox(context.WithValue(WithExplicitAction(ctx), guestPluginRetryKey{}, true), out, id, name, func() bool {
		return s.fixSandboxCurrent(ctx, socket, id, name)
	})
}

// RecoverPendingPolicy resumes cleanup from the status journal on activation.
func (s *Service) RecoverPendingPolicy(scope, socket string) error {
	return s.recoverPendingPolicy(scope, socket)
}

// InstallationAccess returns the exact HTTPS endpoints requested by the
// temporary installer, in display order.
func (s *Service) InstallationAccess() []sbx.PolicyAccess {
	return installerAccess()
}
