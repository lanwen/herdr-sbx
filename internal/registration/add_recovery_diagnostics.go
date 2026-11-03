package registration

import (
	"fmt"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/store"
)

func (r *reconciliation) resolveAbsentAddFailures() error {
	status, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	confirmed := map[string]bool{}
	for _, row := range r.rows {
		if !hasAbsentAddFailure(status, row) {
			continue
		}
		if _, err := r.service.SandboxObservation(r.ctx, r.prerequisites.Socket, row.Sandbox); err != nil {
			return fmt.Errorf("verify recovered registration: %w", err)
		}
		confirmed[store.StatusKey(row.Scope, row.Sandbox.ID)] = true
		confirmed[row.Sandbox.ID] = true
	}
	if len(confirmed) == 0 {
		return nil
	}
	return store.ResolveErrors(func(entry store.ErrorRecord) bool {
		return confirmed[entry.Scope] && absentAddFailure(entry)
	})
}

func hasAbsentAddFailure(status store.StatusStore, row SandboxRow) bool {
	if !row.EntryPresent || row.ProfileID == "" || row.AssociationIssue != "" || row.ObservationPending {
		return false
	}
	for _, entry := range status.Errors {
		if (entry.Scope == row.Sandbox.ID || entry.Scope == store.StatusKey(row.Scope, row.Sandbox.ID)) && absentAddFailure(entry) {
			return true
		}
	}
	return false
}

func absentAddFailure(entry store.ErrorRecord) bool {
	// This exact suffix identifies the old explicit add-verification failure.
	return entry.Code == store.CodeManagement && entry.PluginID == "" && strings.HasSuffix(entry.Detail, "the herdr entry is absent after setup")
}
