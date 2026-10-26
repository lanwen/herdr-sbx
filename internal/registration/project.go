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

// Project setup is part of registration, including upgrades of already associated
// machines. Its failure must not discard the association or add another machine.
func (s *Service) projectReadiness(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, known bool, current func() bool) result {
	s.observeGuestServer(ctx, scope, sb, current)
	p := projectPreparation{service: s, ctx: ctx, scope: scope, state: st, sandbox: sb, item: item, known: known, current: current, outcome: result{Name: sb.Name, Status: "ready", Detail: "already registered"}}
	if !known {
		p.outcome.Detail = "registered"
	}
	if outcome := p.prepareWorkspace(); outcome != nil {
		return *outcome
	}
	p.explicit, _ = ctx.Value(guestPluginRetryKey{}).(bool)
	p.fresh, _ = ctx.Value(freshGuestObservationKey{}).(bool)
	fingerprint, err := guestplugins.DesiredFingerprint(ctx, s.herdr, scope, sb.ID)
	if err != nil {
		return result{Name: sb.Name, Status: "plugin setup required", Detail: "herdr plugin sync could not read the local source: " + err.Error()}
	}
	p.fingerprint = item.Target + "\x00" + sb.ID + "\x00" + fingerprint
	if cached := p.cached(); cached != nil {
		return *cached
	}
	if err := guestplugins.ApplySandbox(ctx, s.herdr, item.Target, scope, sb.ID, current, p.beginAttempt); err != nil {
		return p.save(p.failed(err))
	}
	return p.save(p.outcome)
}

type projectPreparation struct {
	service                        *Service
	ctx                            context.Context
	scope                          string
	state                          *store.State
	sandbox                        sbx.Sandbox
	item                           *store.Tracked
	known                          bool
	current                        func() bool
	outcome                        result
	explicit, fresh                bool
	fingerprint                    string
	attemptStarted, attemptBlocked bool
}

func (p *projectPreparation) prepareWorkspace() *result {
	if !p.item.ProjectWorkspaceDone && (p.sandbox.Workspace != "" || p.item.ProjectWorkspacePending != "") {
		if err := p.service.prepareProjectWorkspace(p.ctx, p.scope, p.state, p.sandbox, p.item, p.current); err != nil {
			if herdr.MachineForwardingUnsupported(err) {
				return &result{Name: p.sandbox.Name, Status: "workspace setup required", DiagnosticCode: store.CodeHerdrCompatibility, Detail: "herdr in this sandbox could not provide machine API forwarding. Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."}
			}
			return &result{Name: p.sandbox.Name, Status: "workspace setup required", Detail: "machine is registered; project workspace: " + err.Error()}
		}
	}
	return nil
}

func (p *projectPreparation) cached() *result {
	if observed := p.item.GuestSync; !p.explicit && !p.fresh && observed != nil && observed.Fingerprint == p.fingerprint && time.Since(observed.CheckedAt) >= 0 && time.Since(observed.CheckedAt) < guestObservationAuditInterval {
		if p.ctx.Err() != nil || p.current == nil || !p.current() {
			return &result{Name: p.sandbox.Name, Status: "stale", Detail: "sandbox identity could not be verified"}
		}
		detail := observed.Detail
		if p.known && observed.Status == "ready" {
			detail = p.outcome.Detail
		}
		return &result{Name: p.sandbox.Name, Status: observed.Status, Detail: detail, PluginFailures: observed.PluginFailures}
	}
	return nil
}

func (p *projectPreparation) save(observed result) result {
	checkedAt := time.Now()
	for i := range observed.PluginFailures {
		if observed.PluginFailures[i].OccurredAt.IsZero() {
			observed.PluginFailures[i].OccurredAt = checkedAt
		}
	}
	latest, fingerprintErr := guestplugins.DesiredFingerprint(p.ctx, p.service.herdr, p.scope, p.sandbox.ID)
	latest = p.item.Target + "\x00" + p.sandbox.ID + "\x00" + latest
	if p.ctx.Err() != nil || p.current == nil || !p.current() {
		return result{Name: p.sandbox.Name, Status: "stale", Detail: "sandbox changed during herdr plugin synchronization"}
	}
	p.item.GuestSync = nil
	if fingerprintErr == nil && latest == p.fingerprint {
		p.item.GuestSync = &store.GuestSyncObservation{Fingerprint: p.fingerprint, CheckedAt: checkedAt, Status: observed.Status, Detail: observed.Detail, PluginFailures: observed.PluginFailures}
	}
	if err := replaceTracked(p.state, p.scope, *p.item); err != nil {
		return result{Name: p.sandbox.Name, Status: "plugin setup required", Detail: "could not save herdr plugin observation: " + err.Error()}
	}
	if err := store.SaveState(*p.state); err != nil {
		return result{Name: p.sandbox.Name, Status: "plugin setup required", Detail: "could not save herdr plugin observation: " + err.Error()}
	}
	return observed
}

func (p *projectPreparation) beginAttempt(id, revision string) (func(error) error, error) {
	if !p.explicit {
		if err := p.checkPreviousAttempt(id, revision); err != nil {
			return nil, err
		}
	}

	// A policy log entry is evidence only if it changed during this
	// attempt and names a destination present in the failed command.
	before, beforeErr := p.service.sbx.BlockedNetworkRequests(p.ctx, p.sandbox.Name)
	started := time.Now()
	allowed, err := store.BeginGuestPluginAttempt(p.scope, p.sandbox.ID, id, revision, p.explicit)
	if err != nil {
		return nil, fmt.Errorf("save herdr plugin attempt: %w", err)
	}
	if !allowed {
		return nil, p.blockedAttempt(id, revision)
	}
	p.attemptStarted = true
	return func(installErr error) error {
		return p.finishAttempt(id, revision, before, beforeErr, started, installErr)
	}, nil
}

func (p *projectPreparation) checkPreviousAttempt(id, revision string) error {
	previous, found, err := store.GuestPluginAttemptFor(p.scope, p.sandbox.ID, id, revision)
	if err != nil {
		return fmt.Errorf("read herdr plugin attempt: %w", err)
	}
	if found {
		p.attemptBlocked = true
		return manualPluginAttemptError(previous, id)
	}
	return nil
}

func (p *projectPreparation) blockedAttempt(id, revision string) error {
	p.attemptBlocked = true
	previous, _, err := store.GuestPluginAttemptFor(p.scope, p.sandbox.ID, id, revision)
	if err != nil {
		return fmt.Errorf("read herdr plugin attempt: %w", err)
	}
	return manualPluginAttemptError(previous, id)
}

func (p *projectPreparation) finishAttempt(id, revision string, before []sbx.NetworkPolicyLog, beforeErr error, started time.Time, installErr error) error {

	if installErr == nil {
		return store.FinishGuestPluginAttempt(p.scope, p.sandbox.ID, id, revision, "")
	}
	failure := guestplugins.FailureFor(herdr.PluginInstall{ID: id}, "", installErr)
	detail := installErr.Error()
	p.addNetworkFailure(&failure, before, beforeErr, started, detail)
	failure.OccurredAt = started
	if err := store.SaveGuestPluginAttemptFailure(p.scope, p.sandbox.ID, id, revision, failure); err != nil {
		return err
	}
	previous, _, err := store.GuestPluginAttemptFor(p.scope, p.sandbox.ID, id, revision)
	if err != nil {
		return err
	}
	if previous.Failure != nil {
		failure = *previous.Failure
	}
	return &guestplugins.FailureError{Failure: failure, Cause: installErr}
}

func (p *projectPreparation) addNetworkFailure(failure *store.PluginFailure, before []sbx.NetworkPolicyLog, beforeErr error, started time.Time, detail string) {
	if beforeErr == nil {
		logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		after, logErr := p.service.sbx.BlockedNetworkRequests(logCtx, p.sandbox.Name)
		cancel()
		if logErr == nil {
			if cause := confirmedNetworkBlock(before, after, p.sandbox.ID, started, detail); cause != "" {
				failure.Code = store.CodeSandboxNetwork
				failure.Detail = cause
			}
		}
	}
}

func (p *projectPreparation) failed(err error) result {
	detail := err.Error()
	status := "plugin setup required"
	if p.attemptStarted || p.attemptBlocked {
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
	return (result{Name: p.sandbox.Name, Status: status, Detail: detail, PluginFailures: failures})
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
	previous := blockedCounts(before, sandboxID)
	current := blockedCounts(after, sandboxID)
	latest := latestNetworkBlocks(after, sandboxID)
	for _, entry := range after {
		if entry.SandboxID != sandboxID || entry.Host == "" {
			continue
		}
		key := entry.Host + "\x00" + entry.Reason
		if current[key] <= previous[key] || latest[key].Before(started.Add(-time.Second)) {
			continue
		}
		if cause := networkBlockCause(entry, failure); cause != "" {
			return cause
		}
	}
	return ""
}
func blockedCounts(entries []sbx.NetworkPolicyLog, sandboxID string) map[string]int {
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.SandboxID == sandboxID {
			counts[entry.Host+"\x00"+entry.Reason] += entry.Count
		}
	}
	return counts
}
func latestNetworkBlocks(entries []sbx.NetworkPolicyLog, sandboxID string) map[string]time.Time {
	latest := map[string]time.Time{}
	for _, entry := range entries {
		if entry.SandboxID != sandboxID || entry.Host == "" {
			continue
		}
		key := entry.Host + "\x00" + entry.Reason
		if entry.LastSeen.After(latest[key]) {
			latest[key] = entry.LastSeen
		}
	}
	return latest
}
func networkBlockCause(entry sbx.NetworkPolicyLog, failure string) string {
	if !strings.Contains(strings.ToLower(failure), strings.ToLower(entry.Host)) {
		return ""
	}
	host := strings.TrimSpace(entry.Host)
	if len(host) > 200 || strings.ContainsFunc(host, unicode.IsControl) {
		return ""
	}
	cause := "Sandbox network access: policy blocked " + host
	if reason := strings.TrimSpace(entry.Reason); reason != "" && len(reason) <= 200 && !strings.ContainsFunc(reason, unicode.IsControl) {
		cause += " (" + reason + ")"
	}
	return cause
}

type workspaceSetup struct {
	service *Service
	ctx     context.Context
	scope   string
	state   *store.State
	sandbox sbx.Sandbox
	item    *store.Tracked
	current func() bool
	cwd     string
}

func (s *Service) prepareProjectWorkspace(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, current func() bool) error {
	w := workspaceSetup{service: s, ctx: ctx, scope: scope, state: st, sandbox: sb, item: item, current: current, cwd: item.ProjectWorkspacePending}
	if err := w.check(); err != nil {
		return err
	}
	if w.cwd == "" {
		var err error
		w.cwd, err = s.projectDirectory(ctx, sb)
		if err != nil {
			return err
		}
	}
	if err := w.check(); err != nil {
		return err
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return err
	}
	panes, output, err := s.herdr.Panes(ctx, item.ProfileID)
	if err != nil {
		return workspaceCommandError("inspect existing workspaces", err, output)
	}
	if containsProjectWorkspace(panes, w.cwd) {
		return w.complete()
	}
	if item.ProjectWorkspacePending != "" {
		return errors.New("previous creation is unresolved; inspect remote workspaces before retrying; no additional workspace was created")
	}
	return w.create()
}
func workspaceCommandError(detail string, err error, output string) error {
	var upstream *herdr.CommandError
	if errors.As(err, &upstream) {
		return fmt.Errorf("%s: %w", detail, err)
	}
	return fmt.Errorf("%s: %s", detail, commandError(err, output))
}
func containsProjectWorkspace(panes []herdr.Pane, cwd string) bool {
	for _, pane := range panes {
		if pane.WorkspaceID != "" && pane.CWD == cwd {
			return true
		}
	}
	return false
}
func (w *workspaceSetup) check() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if w.current == nil || !w.current() {
		return errors.New("sandbox identity could not be verified")
	}
	return nil
}
func (w *workspaceSetup) save() error { return saveTracked(w.state, w.scope, *w.item) }
func (w *workspaceSetup) complete() error {
	if err := w.check(); err != nil {
		return err
	}
	w.item.ProjectWorkspaceDone = true
	w.item.ProjectWorkspacePending = ""
	return w.save()
}
func (w *workspaceSetup) intent() error {
	if err := w.check(); err != nil {
		return err
	}
	w.item.ProjectWorkspacePending = w.cwd
	if err := w.save(); err != nil {
		return err
	}
	if err := w.check(); err != nil {
		w.item.ProjectWorkspacePending = ""
		if saveErr := w.save(); saveErr != nil {
			return saveErr
		}
		return err
	}
	return nil
}
func (w *workspaceSetup) create() error {
	if err := w.intent(); err != nil {
		return err
	}
	if err := CheckAutomaticWork(w.ctx); err != nil {
		return err
	}
	response, output, createErr := w.service.herdr.CreateWorkspace(w.ctx, w.item.ProfileID, w.cwd, w.sandbox.Name)
	if response.Rejected != "" {
		return w.rejected(response.Rejected, createErr)
	}
	if createErr != nil {
		return workspaceCommandError("creation outcome is uncertain", createErr, output)
	}
	if response.ID == "" || response.CWD != w.cwd {
		return errors.New("creation returned no verified project workspace; retry registration to inspect the result")
	}
	return w.complete()
}
func (w *workspaceSetup) rejected(detail string, createErr error) error {
	w.item.ProjectWorkspacePending = ""
	if err := w.save(); err != nil {
		return err
	}
	if createErr != nil {
		return fmt.Errorf("create rejected: %w", createErr)
	}
	return fmt.Errorf("create rejected: %s", detail)
}

func validProjectDirectory(cwd string) bool {
	return path.IsAbs(cwd) && path.Clean(cwd) == cwd && !strings.ContainsFunc(cwd, unicode.IsControl)
}

func (s *Service) projectDirectory(ctx context.Context, sb sbx.Sandbox) (string, error) {
	// Current sbx direct and clone workspaces keep the inventory path inside
	// the guest on macOS/Linux. Older worktrees and Windows require a probe.
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
	done, cwd := initialWorkspaceState(st.Scopes[scope], sb.ID)
	if done {
		return "", nil
	}
	if cwd == "" {
		var err error
		cwd, err = s.initialProjectDirectory(ctx, scope, st, sb, current)
		if err != nil || cwd == "" {
			return "", err
		}
	}
	if err := checkProjectStartup(ctx, current); err != nil {
		return "", err
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return "", err
	}
	if err := s.herdr.StartProjectServer(ctx, sb.Name+".sbx", cwd); err != nil {
		return "", err
	}
	return cwd, nil
}
func initialWorkspaceState(items []store.Tracked, id string) (bool, string) {
	for _, item := range items {
		if item.SandboxID == id && item.ProjectWorkspaceDone {
			return true, ""
		}
	}
	cwd := ""
	for _, item := range items {
		if item.SandboxID == id && item.ProfileID == "" {
			cwd = item.ProjectWorkspacePending
		}
	}
	return false, cwd
}
func checkProjectStartup(ctx context.Context, current func() bool) error {
	if ctx.Err() != nil || current == nil || !current() {
		return errors.New("sandbox changed before project startup")
	}
	return nil
}
func (s *Service) initialProjectDirectory(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, current func() bool) (string, error) {
	if sb.Workspace == "" {
		return "", nil
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return "", err
	}
	if current != nil && !current() {
		return "", context.Canceled
	}
	running, err := s.herdr.GuestServerRunning(ctx, sb.Name+".sbx")
	if err != nil {
		return "", err
	}
	if running {
		return "", nil
	}
	cwd, err := s.projectDirectory(ctx, sb)
	if err != nil {
		return "", err
	}
	if err := checkProjectStartup(ctx, current); err != nil {
		return "", err
	}
	st.Scopes[scope] = append(st.Scopes[scope], store.Tracked{SandboxID: sb.ID, Label: sandboxMachineLabel(sb.Name), Target: sb.Name + ".sbx", ProjectWorkspacePending: cwd})
	if err := store.SaveState(*st); err != nil {
		return "", err
	}
	return cwd, nil
}
