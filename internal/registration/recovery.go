package registration

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

// Recovery never stops a server: live handoff preserves guest panes, and an
// unsupported or failed handoff remains visible for the user to resolve.
func (s *Service) recoverGuestVersion(ctx context.Context, work sandboxWork, progress func(string), force bool) error {
	scope, sb, current := work.Scope, work.Sandbox, work.Current
	if err := checkGuestCurrent(ctx, current); err != nil {
		return err
	}
	local, err := s.herdr.Version(ctx)
	if err != nil {
		return err
	}
	wanted := version.Parse(local)
	if wanted == "" || strings.TrimSpace(local) != "herdr "+wanted {
		return fmt.Errorf("local Herdr must report an exact stable release version")
	}
	return s.recoverGuestVersionTo(ctx, sandboxWork{Scope: scope, Sandbox: sb, Current: current}, progress, force, wanted)
}

func (s *Service) recoverGuestVersionTo(ctx context.Context, work sandboxWork, progress func(string), force bool, wanted string) error {
	scope, sb, current := work.Scope, work.Sandbox, work.Current
	if err := checkGuestCurrent(ctx, current); err != nil {
		return err
	}
	target := sb.Name + ".sbx"
	if err := s.alignGuestExecutable(ctx, target, wanted, current, progress); err != nil {
		return err
	}

	if current != nil && !current() {
		return context.Canceled
	}
	needed, err := s.guestNeedsHandoff(ctx, sandboxWork{Scope: scope, Sandbox: sb, Current: current}, wanted, force)
	if err != nil || !needed {
		return err
	}

	if err := checkGuestCurrent(ctx, current); err != nil {
		return err
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return err
	}
	err = s.herdr.HandoffGuest(ctx, target, wanted)
	s.recordHandoffObservation(ctx, sandboxWork{Scope: scope, Sandbox: sb, Current: current}, wanted, err)
	if err == nil {
		return checkGuestCurrent(ctx, current)
	}

	return err
}

func checkGuestCurrent(ctx context.Context, current func() bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if current != nil && !current() {
		return context.Canceled
	}
	return nil
}
func (s *Service) guestNeedsHandoff(ctx context.Context, work sandboxWork, wanted string, force bool) (bool, error) {
	scope, sb, current := work.Scope, work.Sandbox, work.Current
	if force {
		return true, nil
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return false, err
	}
	running, actual, err := s.herdr.GuestServerVersion(ctx, sb.Name+".sbx")
	if current == nil || current() {
		if err != nil {
			s.recordGuestServerObservation(ctx, scope, sb, false, "")
		} else {
			s.recordGuestServerObservation(ctx, scope, sb, running, actual)
		}
	}
	if err != nil {
		return false, err
	}
	if err := checkGuestCurrent(ctx, current); err != nil {
		return false, err
	}
	return running && actual != wanted, nil
}
func (s *Service) recordHandoffObservation(ctx context.Context, work sandboxWork, wanted string, err error) {
	scope, sb, current := work.Scope, work.Sandbox, work.Current
	if current != nil && !current() {
		return
	}
	// HandoffGuest verifies the running server itself; reuse that result.
	if err == nil {
		s.recordGuestServerObservation(ctx, scope, sb, true, wanted)
	} else {
		s.recordGuestServerObservation(ctx, scope, sb, false, "")
	}
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
	detail, ok := pendingNotification(out, key, scope, id, outcome)
	if !ok {
		return
	}

	s.deliverWarning(ctx, out, key, detail)
}
func (s *Service) deliverWarning(ctx context.Context, out io.Writer, key, detail string) {
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
		cause = "herdr entry association needs attention"
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

func (s *Service) alignGuestExecutable(ctx context.Context, target, wanted string, current func() bool, progress func(string)) error {
	remote, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return err
	}
	old := version.Parse(remote.Version)
	if remote.Missing || remote.Conflict || old == "" || strings.TrimSpace(remote.Version) != "herdr "+old {
		return fmt.Errorf("cannot verify the existing guest Herdr executable")
	}
	if old != wanted {
		if _, err := s.prepareHostVersion(ctx, target, hostPreparation{Wanted: wanted, Previous: old, Current: current, Progress: progress}); err != nil {
			return err
		}
	}
	return nil
}

func pendingNotification(out io.Writer, key, scope, id string, outcome result) (string, bool) {
	entry, found, err := store.PrimaryError(store.StatusKey(scope, id))
	if err != nil {
		fmt.Fprintln(out, "Read sandbox diagnostic:", err)
		return "", false
	}
	if !found || entry.Ref == "" {
		return "", false
	}
	detail := registrationNotification(outcome, entry)
	changed, err := store.WarningTransition(key, detail)
	if err != nil {
		fmt.Fprintln(out, "Save sandbox warning:", err)
		return "", false
	}
	if !changed {
		return "", false
	}
	return detail, true
}
