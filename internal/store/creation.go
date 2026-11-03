package store

import "errors"

// CreationRequest authorizes initial registration of one exact created identity.
// It precedes (and never replaces) the durable Pending machine-add attempt.
type CreationRequest struct {
	Scope     string `json:"scope"`
	SandboxID string `json:"sandbox_id"`
	Name      string `json:"name"`
}

// RequestCreation saves the event before any inventory or guest operation.
func RequestCreation(scope, id, name string) (bool, error) {
	if scope == "" || id == "" || name == "" {
		return false, errors.New("created sandbox has no stable identity")
	}
	accepted := false
	err := UpdateStatusStore(func(s *StatusStore) {
		key := StatusKey(scope, id)
		if s.EnabledChoices[key] == Disabled && s.EnabledRevisions[key] > 0 {
			return
		}
		if previous, ok := s.CreationRequests[key]; ok {
			accepted = previous.Name == name
			return
		}
		s.CreationRequests[key] = CreationRequest{Scope: scope, SandboxID: id, Name: name}
		accepted = true
	})
	return accepted, err
}

// ClearCreationRequest ends pre-add permission without changing user choices.
func ClearCreationRequest(scope, id string) error {
	return UpdateStatusStore(func(s *StatusStore) { delete(s.CreationRequests, StatusKey(scope, id)) })
}

// HandoffCreationRequest serializes add authorization with explicit cancellation.
// Once it succeeds, durable Pending owns verification of the external attempt.
func HandoffCreationRequest(scope, id, name string, requireRequest bool) (bool, error) {
	allowed := false
	err := UpdateStatusStore(func(s *StatusStore) {
		key := StatusKey(scope, id)
		if s.EnabledChoices[key] == Disabled && s.EnabledRevisions[key] > 0 {
			return
		}
		request, present := s.CreationRequests[key]
		if requireRequest && !present {
			return
		}
		if present && (request.Scope != scope || request.SandboxID != id || request.Name != name) {
			return
		}
		delete(s.CreationRequests, key)
		allowed = true
	})
	return allowed, err
}
