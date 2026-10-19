package registration

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

// Recovery never stops a server: live handoff preserves guest panes, and an
// unsupported or failed handoff remains visible for the user to resolve.
func (s *Service) recoverGuestVersion(ctx context.Context, scope string, sb sbx.Sandbox, current func() bool, progress func(string), force bool) error {
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
	return s.recoverGuestVersionTo(ctx, scope, sb, current, progress, force, wanted)
}

func (s *Service) recoverGuestVersionTo(ctx context.Context, scope string, sb sbx.Sandbox, current func() bool, progress func(string), force bool, wanted string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if current != nil && !current() {
		return context.Canceled
	}
	target := sb.Name + ".sbx"
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
		if current == nil || current() {
			if err != nil {
				s.recordGuestServerObservation(ctx, scope, sb, false, "")
			} else {
				s.recordGuestServerObservation(ctx, scope, sb, running, actual)
			}
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current != nil && !current() {
			return context.Canceled
		}
		if !running || actual == wanted {
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if current != nil && !current() {
		return context.Canceled
	}
	err = s.herdr.HandoffGuest(ctx, target, wanted)
	if current == nil || current() {
		// HandoffGuest verifies the running server itself; reuse that result.
		if err == nil {
			s.recordGuestServerObservation(ctx, scope, sb, true, wanted)
		} else {
			s.recordGuestServerObservation(ctx, scope, sb, false, "")
		}
	}
	if err == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if current != nil && !current() {
			return context.Canceled
		}
	}
	return err
}

// Persist the last warning so periodic scans and worker restarts do not repeat
// the same toast. Failed delivery clears it so a later scan can try again.
func (s *Service) notifyResult(ctx context.Context, out io.Writer, scope, id string, outcome result) {
	if id == "" || ctx.Err() != nil {
		return
	}
	key := scope + "/registration/" + id
	if outcome.Status == "stale" || outcome.Status == "retrying" {
		return
	}
	if outcome.Status == "ready" {
		if err := store.ClearWarning(key); err != nil {
			fmt.Fprintln(out, "Clear sandbox warning:", err)
		}
		return
	}
	entry, found, err := store.PrimaryError(store.StatusKey(scope, id))
	if err != nil {
		fmt.Fprintln(out, "Read sandbox diagnostic:", err)
		return
	}
	if !found || entry.Ref == "" {
		return
	}
	detail := registrationNotification(outcome, entry)
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

// Keep notifications useful without repeating build output or repair commands.
func registrationNotification(outcome result, entry store.ErrorRecord) string {
	cause := "Sandbox registration needs attention"
	switch entry.Code {
	case store.CodeOwnershipConflict:
		cause = "Existing herdr machine needs adoption"
	case store.CodeWorkspaceSetup:
		cause = "Project workspace setup failed"
	case store.CodeSandboxSetup:
		cause = "Sandbox setup failed"
	case store.CodeGuestPluginSync:
		cause = "herdr plugin sync failed"
	case store.CodeGuestPluginResponse:
		cause = "Unexpected guest plugin response"
	case store.CodeSandboxNetwork:
		cause = "Network policy blocked herdr plugin setup"
	}
	name := strings.Join(strings.Fields(ansi.Strip(outcome.Name)), " ")
	detail := ansi.Truncate(name, 32, "…") + " · " + entry.Ref + " · " + cause
	if len(outcome.PluginFailures) > 1 {
		detail += " · more in TUI"
	}
	return ansi.Truncate(detail, 120, "…")
}
