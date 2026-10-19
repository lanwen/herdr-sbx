package guestplugins

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// FailureError carries one plugin's cause and recovery through registration.
type FailureError struct {
	Failure store.PluginFailure
	Cause   error
}

func (e *FailureError) Error() string {
	detail := e.Failure.Detail
	if e.Failure.Output != "" {
		detail += ": " + e.Failure.Output
	}
	return detail
}
func (e *FailureError) Unwrap() error { return e.Cause }

// Failures keeps independently actionable outcomes when several plugins fail.
type Failures []store.PluginFailure

func (f Failures) Error() string {
	details := make([]string, 0, len(f))
	for _, failure := range f {
		details = append(details, fmt.Sprintf("%s: %s", failure.PluginID, failure.Detail))
	}
	return strings.Join(details, "; ")
}

// FailureFor builds display data while preserving a previous attempt's identity.
func FailureFor(plugin herdr.PluginInstall, revision string, err error) store.PluginFailure {
	var previous *FailureError
	if errors.As(err, &previous) {
		return previous.Failure
	}
	failure := store.PluginFailure{PluginID: plugin.ID, Code: store.CodeGuestPluginSync, Detail: err.Error()}
	var connection *herdr.GuestPluginConnectionError
	if errors.As(err, &connection) {
		failure.Detail = "herdr-sbx could not open a new SSH connection to inspect this herdr plugin in the sandbox: " + connection.Detail
		failure.Recovery = "The herdr machine and existing workspaces were kept. Check the sandbox connection, then choose Re-sync."
		if connection.HostWorkspacePath != "" {
			failure.Detail = fmt.Sprintf("herdr-sbx was trying to inspect this herdr plugin in the sandbox. sbx refused a new SSH connection before any guest work because the sandbox's saved host workspace path %q no longer exists.", connection.HostWorkspacePath)
			failure.Recovery = "The herdr machine and existing workspaces were kept. Check and restore the saved host workspace path, then choose Re-sync."
		}
	}
	if errors.Is(err, herdr.ErrGuestPluginResponse) {
		failure.Code = store.CodeGuestPluginResponse
	}
	var setup *herdr.GuestPluginSetupError
	if errors.As(err, &setup) {
		failure.Detail = "Guest plugin setup failed: " + setup.Cause.Error()
		failure.Output = setup.Output
	}
	if revision != "" && failure.Recovery == "" {
		failure.Recovery, failure.Command = manualRecovery(plugin, revision, false)
	}
	return failure
}
