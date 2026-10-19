package registration

import (
	"context"
	"errors"
	"fmt"
	"path"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Project setup is part of registration, including upgrades of already owned
// machines. Its failure must not discard ownership or add another machine.
func (s *Service) projectReadiness(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, known bool, current func() bool) result {
	s.observeGuestServer(ctx, scope, sb, current)
	outcome := result{Name: sb.Name, Status: "ready", Detail: "already registered"}
	if !known {
		// Machine add already prepared the remote server, and registration
		// verified the returned profile in the local catalog.
		outcome.Detail = "registered"
	}
	if !item.ProjectWorkspaceDone && (sb.Workspace != "" || item.ProjectWorkspacePending != "") {
		if err := s.prepareProjectWorkspace(ctx, scope, st, sb, item, current); err != nil {
			if herdr.MachineForwardingUnsupported(err) {
				return result{Name: sb.Name, Status: "workspace setup required", DiagnosticCode: store.CodeHerdrCompatibility, Detail: "herdr in this sandbox could not provide machine API forwarding. Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."}
			}
			return result{Name: sb.Name, Status: "workspace setup required", Detail: "machine is registered; project workspace: " + err.Error()}
		}
	}
	explicit, _ := ctx.Value(guestPluginRetryKey{}).(bool)
	fresh, _ := ctx.Value(freshGuestObservationKey{}).(bool)
	fingerprint, err := guestplugins.DesiredFingerprint(ctx, s.herdr, scope, sb.ID)
	if err != nil {
		return result{Name: sb.Name, Status: "plugin setup required", Detail: "herdr plugin sync could not read the local source: " + err.Error()}
	}
	fingerprint = item.ProfileID + "\x00" + item.Target + "\x00" + sb.ID + "\x00" + fingerprint
	if observed := item.GuestSync; !explicit && !fresh && observed != nil && observed.Fingerprint == fingerprint && time.Since(observed.CheckedAt) >= 0 && time.Since(observed.CheckedAt) < guestObservationAuditInterval {
		if ctx.Err() != nil || current == nil || !current() {
			return result{Name: sb.Name, Status: "stale", Detail: "sandbox identity could not be verified"}
		}
		detail := observed.Detail
		if known && observed.Status == "ready" {
			detail = outcome.Detail
		}
		return result{Name: sb.Name, Status: observed.Status, Detail: detail, PluginFailures: observed.PluginFailures}
	}
	saveObservation := func(observed result) result {
		checkedAt := time.Now()
		for i := range observed.PluginFailures {
			if observed.PluginFailures[i].OccurredAt.IsZero() {
				observed.PluginFailures[i].OccurredAt = checkedAt
			}
		}
		latest, fingerprintErr := guestplugins.DesiredFingerprint(ctx, s.herdr, scope, sb.ID)
		latest = item.ProfileID + "\x00" + item.Target + "\x00" + sb.ID + "\x00" + latest
		if ctx.Err() != nil || current == nil || !current() {
			return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed during herdr plugin synchronization"}
		}
		item.GuestSync = nil
		if fingerprintErr == nil && latest == fingerprint {
			item.GuestSync = &store.GuestSyncObservation{Fingerprint: fingerprint, CheckedAt: checkedAt, Status: observed.Status, Detail: observed.Detail, PluginFailures: observed.PluginFailures}
		}
		if err := replaceTracked(st, scope, *item); err != nil {
			return result{Name: sb.Name, Status: "plugin setup required", Detail: "could not save herdr plugin observation: " + err.Error()}
		}
		if err := store.SaveState(*st); err != nil {
			return result{Name: sb.Name, Status: "plugin setup required", Detail: "could not save herdr plugin observation: " + err.Error()}
		}
		return observed
	}
	attemptStarted := false
	attemptBlocked := false
	attempt := func(id, revision string) (func(error) error, error) {
		if !explicit {
			previous, found, err := store.GuestPluginAttemptFor(scope, sb.ID, id, revision)
			if err != nil {
				return nil, fmt.Errorf("read herdr plugin attempt: %w", err)
			}
			if found {
				attemptBlocked = true
				return nil, manualPluginAttemptError(previous, id)
			}
		}
		// A policy log entry is evidence only if it changed during this
		// attempt and names a destination present in the failed command.
		before, beforeErr := s.sbx.BlockedNetworkRequests(ctx, sb.Name)
		started := time.Now()
		allowed, err := store.BeginGuestPluginAttempt(scope, sb.ID, id, revision, explicit)
		if err != nil {
			return nil, fmt.Errorf("save herdr plugin attempt: %w", err)
		}
		if !allowed {
			attemptBlocked = true
			previous, _, err := store.GuestPluginAttemptFor(scope, sb.ID, id, revision)
			if err != nil {
				return nil, fmt.Errorf("read herdr plugin attempt: %w", err)
			}
			return nil, manualPluginAttemptError(previous, id)
		}
		attemptStarted = true
		return func(result error) error {
			if result == nil {
				return store.FinishGuestPluginAttempt(scope, sb.ID, id, revision, "")
			}
			failure := guestplugins.FailureFor(herdr.PluginInstall{ID: id}, "", result)
			detail := result.Error()
			if beforeErr == nil {
				logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				after, logErr := s.sbx.BlockedNetworkRequests(logCtx, sb.Name)
				cancel()
				if logErr == nil {
					if cause := confirmedNetworkBlock(before, after, sb.ID, started, detail); cause != "" {
						failure.Code = store.CodeSandboxNetwork
						failure.Detail = cause
					}
				}
			}
			failure.OccurredAt = started
			if err := store.SaveGuestPluginAttemptFailure(scope, sb.ID, id, revision, failure); err != nil {
				return err
			}
			previous, _, err := store.GuestPluginAttemptFor(scope, sb.ID, id, revision)
			if err != nil {
				return err
			}
			if previous.Failure != nil {
				failure = *previous.Failure
			}
			return &guestplugins.FailureError{Failure: failure, Cause: result}
		}, nil
	}
	if err := guestplugins.ApplySandbox(ctx, s.herdr, item.Target, scope, sb.ID, current, attempt); err != nil {
		detail := err.Error()
		status := "plugin setup required"
		if attemptStarted || attemptBlocked {
			status = "manual intervention required"
		}
		var failures guestplugins.Failures
		if errors.As(err, &failures) {
			detail = fmt.Sprintf("%d herdr plugin needs attention", len(failures))
			if len(failures) > 1 {
				detail = fmt.Sprintf("%d herdr plugins need attention", len(failures))
			}
			if status == "manual intervention required" {
				detail = fmt.Sprintf("%d herdr plugin needs manual intervention", len(failures))
				if len(failures) > 1 {
					detail = fmt.Sprintf("%d herdr plugins need manual intervention", len(failures))
				}
			}
		}
		return saveObservation(result{Name: sb.Name, Status: status, Detail: detail, PluginFailures: failures})
	}

	return saveObservation(outcome)
}

// A bounded audit detects manual guest changes without probing on every catch-up.
const guestObservationAuditInterval = 15 * time.Minute

type freshGuestObservationKey struct{}

// WithFreshGuestObservation invalidates the saved observation for a real sandbox
// lifecycle start or event-stream reconnect. Ordinary catch-up should not use it.
func WithFreshGuestObservation(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshGuestObservationKey{}, true)
}

type guestPluginRetryKey struct{}

func manualPluginAttemptError(previous store.GuestPluginAttempt, id string) error {
	if previous.Failure != nil {
		return &guestplugins.FailureError{Failure: *previous.Failure}
	}
	detail := previous.Detail
	if detail == "" {
		detail = "the previous attempt did not finish; inspect the sandbox"
	}
	return &guestplugins.FailureError{Failure: store.PluginFailure{PluginID: id, Code: store.CodeGuestPluginSync, Detail: detail, Recovery: "Inspect and repair the plugin inside the sandbox, then choose Re-sync.", OccurredAt: previous.StartedAt}}
}

func confirmedNetworkBlock(before, after []sbx.NetworkPolicyLog, sandboxID string, started time.Time, failure string) string {
	previous := map[string]int{}
	for _, entry := range before {
		if entry.SandboxID == sandboxID {
			previous[entry.Host+"\x00"+entry.Reason] += entry.Count
		}
	}
	current := map[string]int{}
	latest := map[string]time.Time{}
	for _, entry := range after {
		if entry.SandboxID != sandboxID || entry.Host == "" {
			continue
		}
		key := entry.Host + "\x00" + entry.Reason
		current[key] += entry.Count
		if entry.LastSeen.After(latest[key]) {
			latest[key] = entry.LastSeen
		}
	}
	for _, entry := range after {
		if entry.SandboxID != sandboxID || entry.Host == "" {
			continue
		}
		key := entry.Host + "\x00" + entry.Reason
		if current[key] <= previous[key] || latest[key].Before(started.Add(-time.Second)) {
			continue
		}
		if !strings.Contains(strings.ToLower(failure), strings.ToLower(entry.Host)) {
			continue
		}
		host := strings.TrimSpace(entry.Host)
		if len(host) > 200 || strings.ContainsFunc(host, unicode.IsControl) {
			continue
		}
		cause := "Sandbox network access: policy blocked " + host
		if reason := strings.TrimSpace(entry.Reason); reason != "" && len(reason) <= 200 && !strings.ContainsFunc(reason, unicode.IsControl) {
			cause += " (" + reason + ")"
		}
		return cause
	}
	return ""
}

func (s *Service) prepareProjectWorkspace(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, current func() bool) error {
	checkCurrent := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == nil || !current() {
			return errors.New("sandbox identity could not be verified")
		}
		return nil
	}
	save := func() error {
		if err := replaceTracked(st, scope, *item); err != nil {
			return err
		}
		return store.SaveState(*st)
	}
	complete := func() error {
		if err := checkCurrent(); err != nil {
			return err
		}
		item.ProjectWorkspaceDone = true
		item.ProjectWorkspacePending = ""
		return save()
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	cwd := item.ProjectWorkspacePending
	if cwd == "" {
		var err error
		cwd, err = s.projectDirectory(ctx, sb)
		if err != nil {
			return err
		}
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	panes, output, err := s.herdr.Panes(ctx, item.ProfileID)
	if err != nil {
		var upstream *herdr.CommandError
		if errors.As(err, &upstream) {
			return fmt.Errorf("inspect existing workspaces: %w", err)
		}
		return fmt.Errorf("inspect existing workspaces: %s", commandError(err, output))
	}
	for _, pane := range panes {
		if pane.WorkspaceID != "" && pane.CWD == cwd {
			return complete()
		}
	}
	if item.ProjectWorkspacePending != "" {
		return errors.New("previous creation is unresolved; inspect remote workspaces before retrying; no additional workspace was created")
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	item.ProjectWorkspacePending = cwd
	if err := save(); err != nil {
		return err
	}
	if err := checkCurrent(); err != nil {
		item.ProjectWorkspacePending = ""
		if saveErr := save(); saveErr != nil {
			return saveErr
		}
		return err
	}
	response, output, createErr := s.herdr.CreateWorkspace(ctx, item.ProfileID, cwd, sb.Name)
	if response.Rejected != "" {
		item.ProjectWorkspacePending = ""
		if err := save(); err != nil {
			return err
		}
		if createErr != nil {
			return fmt.Errorf("create rejected: %w", createErr)
		}
		return fmt.Errorf("create rejected: %s", response.Rejected)
	}
	if createErr != nil {
		var upstream *herdr.CommandError
		if errors.As(createErr, &upstream) {
			return fmt.Errorf("creation outcome is uncertain: %w", createErr)
		}
		return fmt.Errorf("creation outcome is uncertain: %s", commandError(createErr, output))
	}
	if response.ID == "" || response.CWD != cwd {
		return errors.New("creation returned no verified project workspace; retry registration to inspect the result")
	}
	return complete()
}

func validProjectDirectory(cwd string) bool {
	return path.IsAbs(cwd) && path.Clean(cwd) == cwd && !strings.ContainsFunc(cwd, unicode.IsControl)
}

func (s *Service) projectDirectory(ctx context.Context, sb sbx.Sandbox) (string, error) {
	// Current sbx direct and clone workspaces keep the inventory path inside
	// the guest on macOS/Linux. Legacy worktrees and Windows require a probe.
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && !sb.WorkspaceNeedsProbe && validProjectDirectory(sb.Workspace) {
		return sb.Workspace, nil
	}
	output, err := s.sbx.WorkspaceDirectory(ctx, sb.Name)
	if err != nil {
		return "", fmt.Errorf("resolve guest directory: %s", commandError(err, output))
	}
	cwd := strings.TrimSuffix(output, "\n")
	if !validProjectDirectory(cwd) {
		return "", errors.New("guest returned an invalid project directory")
	}
	return cwd, nil
}

// prepareInitialProjectWorkspace runs before machine add can bootstrap a guest
// in its SSH login directory. Persist the startup intent so failed or uncertain
// attempts must verify the same project on retry, never create another workspace.
func (s *Service) prepareInitialProjectWorkspace(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, current func() bool) (string, error) {
	cwd := ""
	for _, item := range st.Scopes[scope] {
		if item.SandboxID == sb.ID && item.ProfileID == "" {
			cwd = item.ProjectWorkspacePending
		}
	}
	if cwd == "" {
		if sb.Workspace == "" {
			return "", nil
		}
		running, err := s.herdr.GuestServerRunning(ctx, sb.Name+".sbx")
		if err != nil {
			return "", err
		}
		if running {
			return "", nil
		}
		cwd, err = s.projectDirectory(ctx, sb)
		if err != nil {
			return "", err
		}
		if ctx.Err() != nil || current == nil || !current() {
			return "", errors.New("sandbox changed before project startup")
		}
		st.Scopes[scope] = append(st.Scopes[scope], store.Tracked{SandboxID: sb.ID, Label: sandboxMachineLabel(sb.Name), Target: sb.Name + ".sbx", ProjectWorkspacePending: cwd})
		if err := store.SaveState(*st); err != nil {
			return "", err
		}
	}
	if ctx.Err() != nil || current == nil || !current() {
		return "", errors.New("sandbox changed before project startup")
	}
	if err := s.herdr.StartProjectServer(ctx, sb.Name+".sbx", cwd); err != nil {
		return "", err
	}
	return cwd, nil
}
