package lifecycle

import (
	"fmt"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Persist on reception, including while Management is OFF or registration busy.
func (s *lifecycleScheduler) persistCreationEvent(event sbx.LifecycleEvent) bool {
	switch strings.ToLower(event.Action) {
	case "deleted":
		s.mu.Lock()
		s.retired[event.SandboxID] = true
		delete(s.deferredStarts, event.SandboxID)
		s.mu.Unlock()
		return s.clearCreation(event.SandboxID)
	case "created":
		if s.isRetired(event.SandboxID) {
			return false
		}
		accepted, err := store.RequestCreation(s.scope, event.SandboxID, event.SandboxName)
		if err != nil {
			fmt.Fprintln(s.out, "Created sandbox registration request was not saved:", err)
			s.deferStart(event)
			return false
		}
		return accepted
	default:
		return true
	}
}

func (s *lifecycleScheduler) clearCreation(id string) bool {
	if err := store.ClearCreationRequest(s.scope, id); err != nil {
		fmt.Fprintln(s.out, "Created sandbox registration request was not cleared:", err)
		return false
	}
	return true
}

// Retain the request while Pending owns verification; it grants no replay.
func (s *lifecycleScheduler) creationHandedOff(event sbx.LifecycleEvent) bool {
	if !strings.EqualFold(event.Action, "created") {
		return false
	}
	state, err := store.LoadState()
	if err != nil {
		fmt.Fprintln(s.out, "Created sandbox registration was deferred:", err)
		s.deferStart(event)
		return true
	}
	for _, item := range state.Scopes[s.scope] {
		if item.SandboxID == event.SandboxID && item.Pending {
			return true
		}
	}
	return false
}

func (s *lifecycleScheduler) restoreCreationRequests() {
	saved, err := store.ReadStatusStore()
	if err != nil {
		fmt.Fprintln(s.out, "Created sandbox registration requests could not be read:", err)
		return
	}
	for key, request := range saved.CreationRequests {
		if request.Scope != s.scope || key != store.StatusKey(s.scope, request.SandboxID) {
			continue
		}
		if s.isRetired(request.SandboxID) || (saved.EnabledChoices[key] == store.Disabled && saved.EnabledRevisions[key] > 0) {
			s.clearCreation(request.SandboxID)
			continue
		}
		s.deferStart(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "created", SandboxID: request.SandboxID, SandboxName: request.Name})
	}
}
