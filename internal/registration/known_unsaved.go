package registration

import (
	"context"
	"errors"
	"github.com/lanwen/herdr-sbx/internal/process"
	"os/exec"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/store"
)

// The observed CLI no-save marker is useful only with a failed, unacknowledged
// transient setup and a fresh empty catalog; unknown outcomes remain Pending.
func (a *registrationAttempt) settleKnownUnsaved(output string, commandErr error) *result {
	if !a.catalogAbsent || a.pending.PendingProfileID != "" || !knownUnsavedFailure(output, commandErr) {
		return nil
	}

	if !a.creationRetryCurrent() {
		return nil
	}
	machines, _, err := a.service.herdr.ListMachines(a.ctx)
	if err != nil || a.addCatalogConflict(machines) != "" {
		return nil
	}
	if !a.creationRetryCurrent() {
		return nil
	}
	if err = clearUnperformedAdd(a.state, a.scope, a.sandbox.ID); err != nil {
		return nil
	}
	for i := range a.state.Scopes[a.scope] {
		if a.state.Scopes[a.scope][i].SandboxID == a.sandbox.ID {
			a.state.Scopes[a.scope][i].ProjectWorkspaceDone = false
		}
	}
	if err = store.SaveState(*a.state); err != nil {
		return &result{Name: a.sandbox.Name, Status: "association warning", Detail: "Herdr did not save the connection, but its pending attempt could not be settled: " + err.Error()}
	}
	return &result{Name: a.sandbox.Name, Status: "waiting for provisioning", Detail: "Herdr did not save the connection: " + registrationEvidence(commandError(commandErr, output)) + ". The plugin will retry automatically."}
}

func knownUnsavedFailure(output string, err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 || errors.Is(err, process.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return strings.Contains(strings.ToLower(output), "machine was not saved") && transientAddFailure(output, err)
}

func (a *registrationAttempt) creationRetryCurrent() bool {
	if a.current == nil || !a.current() {
		return false
	}
	allowed, err := store.HandoffCreationRequest(a.scope, a.sandbox.ID, a.sandbox.Name, true)
	return err == nil && allowed
}
