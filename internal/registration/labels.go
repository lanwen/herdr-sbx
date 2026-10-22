package registration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func sandboxMachineLabel(name string) string { return "sbx · " + name }

// The SSH target retains the sandbox name independently of its display label,
// so custom labels do not affect sandbox identity.
func SandboxName(item store.Tracked) string {
	if !strings.HasSuffix(item.Target, ".sbx") {
		return ""
	}
	name := strings.TrimSuffix(item.Target, ".sbx")
	if name == "" || strings.ContainsAny(name, " /\\\t\r\n@:") {
		return ""
	}
	return name
}

func associatedTrackedMachine(machines []herdr.Machine, item store.Tracked) *herdr.Machine {
	var found *herdr.Machine
	for i := range machines {
		if machines[i].Target == item.Target {
			if found != nil {
				return nil
			}
			found = &machines[i]
		}
	}
	if found == nil || found.ID != item.ProfileID || !usesDefaultSession(found.Session) {
		return nil
	}
	return found
}

func (s *Service) recoverMachineLabel(ctx context.Context, scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox, item *store.Tracked, current func() bool) error {
	wanted := item.PendingLabel
	owned := associatedTrackedMachine(machines, *item)
	if owned == nil || SandboxName(*item) != sb.Name {
		return errors.New("saved machine identity no longer matches this sandbox")
	}
	if wanted == "" {
		item.Label = owned.Label
		if err := replaceTracked(st, scope, *item); err != nil {
			return err
		}
		return store.SaveState(*st)
	}
	for _, candidate := range machines {
		if candidate.ID != item.ProfileID && candidate.Label == wanted {
			return errors.New("another machine already uses the sandbox label")
		}
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == nil || !current() {
			return errors.New("sandbox changed before label recovery")
		}
		return nil
	}
	save := func() error {
		if err := replaceTracked(st, scope, *item); err != nil {
			return err
		}
		return store.SaveState(*st)
	}
	if err := check(); err != nil {
		return err
	}
	if owned.Label != wanted {
		item.PendingLabel = wanted
		if err := save(); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		output, err := s.herdr.RenameMachine(ctx, item.ProfileID, wanted)
		if err != nil {
			return fmt.Errorf("rename associated entry: %s", commandError(err, output))
		}
		updated, output, err := s.herdr.ListMachines(ctx)
		if err != nil {
			return fmt.Errorf("verify machine label: %s", commandError(err, output))
		}
		observed := associatedMachineByID(updated, item.ProfileID, wanted, item.Target)
		if observed == nil || observed.Label != wanted {
			return errors.New("machine label change could not be verified")
		}
	}
	if err := check(); err != nil {
		return err
	}
	item.Label, item.PendingLabel = wanted, ""
	return save()
}
