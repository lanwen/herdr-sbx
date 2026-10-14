package registration

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

// Recovery never stops a server: live handoff preserves guest panes, and an
// unsupported or failed handoff remains visible for the user to resolve.
func (s *Service) recoverGuestVersion(ctx context.Context, target string, current func() bool, progress func(string), force bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if current != nil && !current() {
		return context.Canceled
	}
	local, err := s.herdr.Version(ctx)
	if err != nil {
		return err
	}
	wanted := version.Parse(local)
	if wanted == "" || strings.TrimSpace(local) != "herdr "+wanted {
		return fmt.Errorf("local Herdr must report an exact stable release version")
	}
	remote, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return err
	}
	old := version.Parse(remote.Version)
	if remote.Missing || remote.Conflict || old == "" || strings.TrimSpace(remote.Version) != "herdr "+old {
		return fmt.Errorf("cannot verify the existing guest Herdr executable")
	}
	if old != wanted {
		if _, err := s.prepareHostVersion(ctx, target, wanted, old, current, progress); err != nil {
			return err
		}
	}
	if current != nil && !current() {
		return context.Canceled
	}
	if !force {
		running, actual, err := s.herdr.GuestServerVersion(ctx, target)
		if err != nil {
			return err
		}
		if !running || actual == wanted {
			return nil
		}
	}
	return s.herdr.HandoffGuest(ctx, target, wanted)
}

// Persist the last warning so periodic scans and worker restarts do not repeat
// the same toast. Failed delivery clears it so a later scan can try again.
func (s *Service) notifyResult(ctx context.Context, out io.Writer, scope, id string, outcome result) {
	if id == "" || ctx.Err() != nil {
		return
	}
	key := scope + "/registration/" + id
	if outcome.Status == "stale" {
		return
	}
	if outcome.Status == "ready" {
		if err := store.ClearWarning(key); err != nil {
			fmt.Fprintln(out, "Clear sandbox warning:", err)
		}
		return
	}
	detail := outcome.Name + ": " + outcome.Status + ". " + outcome.Detail
	changed, err := store.WarningTransition(key, detail)
	if err != nil {
		fmt.Fprintln(out, "Save sandbox warning:", err)
		return
	}
	if !changed {
		return
	}
	if err := s.herdr.Notify(ctx, "herdr-sbx", detail); err != nil {
		fmt.Fprintln(out, "Show sandbox warning:", err)
		if clearErr := store.ClearWarning(key); clearErr != nil {
			fmt.Fprintln(out, "Clear undelivered warning:", clearErr)
		}
	}
}
