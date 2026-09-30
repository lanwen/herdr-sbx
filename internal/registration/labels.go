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
// including state written before labels had a prefix.
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

func ownedTrackedMachine(machines []herdr.Machine, item store.Tracked) *herdr.Machine {
	if owned := ownedMachineByID(machines, item.ProfileID, item.Label, item.Target); owned != nil {
		return owned
	}
	if item.PendingLabel != "" && item.PendingLabel == sandboxMachineLabel(SandboxName(item)) {
		return ownedMachineByID(machines, item.ProfileID, item.PendingLabel, item.Target)
	}
	return nil
}

func (s *Service) migrateMachineLabel(ctx context.Context, scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox, item *store.Tracked, current func() bool) error {
	wanted := sandboxMachineLabel(sb.Name)
	owned := ownedTrackedMachine(machines, *item)
	if owned == nil || SandboxName(*item) != sb.Name {
		return errors.New("saved machine is no longer the owned profile")
	}
	if item.Label == wanted && item.PendingLabel == "" {
		return nil
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
			return errors.New("sandbox changed before label migration")
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
			return fmt.Errorf("rename owned machine: %s", commandError(err, output))
		}
		updated, output, err := s.herdr.ListMachines(ctx)
		if err != nil {
			return fmt.Errorf("verify machine label: %s", commandError(err, output))
		}
		if ownedMachineByID(updated, item.ProfileID, wanted, item.Target) == nil {
			return errors.New("machine label change could not be verified")
		}
	}
	if err := check(); err != nil {
		return err
	}
	item.Label, item.PendingLabel = wanted, ""
	return save()
}
