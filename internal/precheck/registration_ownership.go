package precheck

import "github.com/lanwen/herdr-sbx/internal/store"

func (i *Inspector) registrationOwnership(scope string) []Check {
	if scope == "" {
		return nil
	}
	state, err := store.LoadState()
	if err != nil {
		return []Check{{"Registration association", Unknown, "saved sandbox-to-herdr associations could not be read", "Check the plugin state before enabling more sandboxes."}}
	}
	for _, item := range state.Scopes[scope] {
		if item.Pending && item.ProfileID == "" {
			return []Check{{"Registration association", Unknown, "entry creation is not confirmed for " + item.Label, "Check the herdr machine catalog. Retry Enable if this sandbox has no entry."}}
		}
	}
	return nil
}
