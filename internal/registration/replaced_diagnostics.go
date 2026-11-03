package registration

import (
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func (r *reconciliation) resolveReplacedSetupFailures() error {
	status, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	retired := map[string]bool{}
	for _, entry := range status.Errors {
		cached, found := status.Sandboxes[entry.Scope]
		if entry.Code == store.CodeSandboxSetup && found && entry.Scope == store.StatusKey(cached.Scope, cached.SandboxID) && replacedSandbox(r.prerequisites.Scope, cached, r.sandboxes) {
			retired[entry.Scope] = true
		}
	}
	if len(retired) == 0 {
		return nil
	}
	return store.ResolveErrors(func(entry store.ErrorRecord) bool {
		return entry.Code == store.CodeSandboxSetup && retired[entry.Scope]
	})
}

// Absence alone is insufficient: the complete inventory must prove name reuse.
func replacedSandbox(scope string, cached store.SandboxStatus, live []sbx.Sandbox) bool {
	if cached.Scope != scope || cached.SandboxID == "" || cached.Name == "" {
		return false
	}
	matches := 0
	for _, sb := range live {
		if sb.ID == cached.SandboxID {
			return false
		}
		if sb.Name == cached.Name {
			if sb.ID == "" {
				return false
			}
			matches++
		}
	}
	return matches == 1
}
