package precheck

import "github.com/lanwen/herdr-sbx/internal/store"

func (i *Inspector) registrationOwnership(scope string) []Check {
	if scope == "" {
		return nil
	}
	state, err := store.LoadState()
	if err != nil {
		return []Check{{"Registration ownership", Unknown, "saved registration state could not be read", "Inspect plugin state before registering more sandboxes."}}
	}
	for _, item := range state.Scopes[scope] {
		if item.Pending && item.ProfileID == "" {
			return []Check{{"Registration ownership", Unknown, "an unresolved registration intent remains for " + item.Label, "Inspect the Herdr machine catalog before retrying."}}
		}
	}
	return nil
}
