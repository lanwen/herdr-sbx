package registration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Adoption identifies the exact sandbox and existing profile shown to the user.
// Adoption grants lifecycle ownership, including removal after sandbox deletion.
type Adoption struct {
	Scope, Socket string
	Sandbox       sbx.Sandbox
	Machine       herdr.Machine
}

// AdoptionChoices lists unambiguous target matches. A label alone cannot prove
// identity; the managed SSH alias and default remote session must match too.
func (s *Service) AdoptionChoices(ctx context.Context) ([]Adoption, error) {
	p := s.Inspect()
	for _, name := range []string{"Herdr", "Sandbox daemon", "Sandbox inventory", "Sandbox SSH"} {
		if !hasLevel(p.Checks, name, precheck.Ready) {
			return nil, fmt.Errorf("adoption requires ready %s checks", name)
		}
	}
	if p.Scope == "" || p.Socket == "" {
		return nil, errors.New("sandbox daemon scope is unavailable")
	}
	sandboxes, err := s.inventory(ctx, p.Socket)
	if err != nil {
		return nil, err
	}
	machines, _, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return nil, err
	}
	state, err := store.LoadState()
	if err != nil {
		return nil, err
	}
	return adoptionChoices(p.Scope, p.Socket, sandboxes, machines, state), nil
}

func adoptionChoices(scope, socket string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State) []Adoption {
	var choices []Adoption
	for _, sandbox := range sandboxes {
		if sandbox.ID == "" || sandbox.Name == "" || !strings.EqualFold(sandbox.Status, "running") {
			continue
		}
		target := sandbox.Name + ".sbx"
		if SandboxName(store.Tracked{Target: target}) != sandbox.Name {
			continue
		}
		var matched *herdr.Machine
		count := 0
		for i := range machines {
			if machines[i].Target == target {
				matched = &machines[i]
				count++
			}
		}
		if count != 1 || matched.ID == "" || !usesDefaultSession(matched.Session) {
			continue
		}
		eligible := true
		for _, other := range machines {
			if other.ID != matched.ID && other.Label == sandboxMachineLabel(sandbox.Name) {
				eligible = false
			}
		}
		for trackedScope, entries := range state.Scopes {
			for _, entry := range entries {
				if trackedScope == scope && entry.SandboxID == sandbox.ID && entry.ProfileID == "" && entry.Target == target {
					continue
				} // Resolve a matching unfinished intent explicitly.
				if entry.ProfileID == matched.ID || entry.Target == target || (trackedScope == scope && entry.SandboxID == sandbox.ID) {
					eligible = false
				}
			}
		}
		if eligible {
			choices = append(choices, Adoption{Scope: scope, Socket: socket, Sandbox: sandbox, Machine: *matched})
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Sandbox.Name < choices[j].Sandbox.Name })
	return choices
}

// Adopt records ownership only: no machine add/remove, SSH, or workspace action.
// Revalidate the selection under the same lock used by the background worker.
func (s *Service) Adopt(ctx context.Context, selected Adoption) error {
	unlock, err := store.LockState()
	if err != nil {
		return err
	}
	defer unlock()
	choices, err := s.AdoptionChoices(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, choice := range choices {
		if choice.Scope == selected.Scope && choice.Socket == selected.Socket && choice.Sandbox.ID == selected.Sandbox.ID && choice.Sandbox.Name == selected.Sandbox.Name && choice.Machine == selected.Machine {
			found = true
			break
		}
	}
	if !found {
		return errors.New("sandbox, machine or ownership changed; refresh the adoption list")
	}
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	item := store.Tracked{}
	for _, entry := range state.Scopes[selected.Scope] {
		if entry.SandboxID == selected.Sandbox.ID {
			item = entry
			break
		}
	}
	item.SandboxID = selected.Sandbox.ID
	item.ProfileID = selected.Machine.ID
	item.Label = selected.Machine.Label
	item.CustomLabel = true
	item.Target = selected.Machine.Target
	item.Pending = false
	item.PendingLabel = ""
	if err := replaceTracked(&state, selected.Scope, item); err != nil {
		state.Scopes[selected.Scope] = append(state.Scopes[selected.Scope], item)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.SaveState(state); err != nil {
		return err
	}
	if err := store.SetManagement(selected.Scope, selected.Sandbox.ID, store.Watched); err != nil {
		return err
	}
	if err := store.RecordSandboxStatus(selected.Scope, selected.Sandbox, "adopted", "existing machine is now managed; registration will check readiness", false); err != nil {
		return fmt.Errorf("ownership saved, but status update failed: %w", err)
	}
	return nil
}

// AdoptMachine provides an explicit CLI equivalent of selecting a listed pair.
func (s *Service) AdoptMachine(ctx context.Context, sandboxName, profileID string) error {
	choices, err := s.AdoptionChoices(ctx)
	if err != nil {
		return err
	}
	for _, choice := range choices {
		if choice.Sandbox.Name == sandboxName && choice.Machine.ID == profileID {
			return s.Adopt(ctx, choice)
		}
	}
	return errors.New("no eligible sandbox/machine pair; adoption needs a unique matching SSH target, default session and no conflicting ownership")
}
