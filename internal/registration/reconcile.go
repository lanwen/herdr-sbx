package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

const minHerdrVersion = herdr.MinimumVersion

type result struct {
	Name           string
	Status         string
	Detail         string
	DiagnosticCode string
	PluginFailures []store.PluginFailure
	RetryError     error
}

func associatedMachineByID(machines []herdr.Machine, id, label, target string) *herdr.Machine {
	for i := range machines {
		candidate := &machines[i]
		if candidate.ID == id && candidate.Target == target && usesDefaultSession(candidate.Session) {
			return candidate
		}
	}
	return nil
}

func usesDefaultSession(session string) bool { return strings.EqualFold(session, "default") }

func machineByID(machines []herdr.Machine, id string) *herdr.Machine {
	for i := range machines {
		if machines[i].ID == id {
			return &machines[i]
		}
	}
	return nil
}

func (s *Service) Reconcile(out io.Writer) error {
	return s.ReconcileSelected(out, nil)
}

// Manual actions stop their in-flight download/SSH work before returning on a signal.
func (s *Service) ReconcileSelected(out io.Writer, match func(sbx.Sandbox) bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.reconcileMatching(WithExplicitAction(ctx), out, match, nil)
}

func (s *Service) ReconcileContext(ctx context.Context, out io.Writer) error {
	return s.reconcileMatching(ctx, out, nil, nil)
}

func (s *Service) reconcileMatching(ctx context.Context, out io.Writer, match func(sbx.Sandbox) bool, current func(sbx.Sandbox) bool) error {
	return s.reconcileWithInstallation(ctx, out, match, current, installationOptions{})
}

func (s *Service) reconcileWithInstallation(ctx context.Context, out io.Writer, match func(sbx.Sandbox) bool, current func(sbx.Sandbox) bool, installation installationOptions) error {
	if err := CheckAutomaticWork(ctx); err != nil {
		return err
	}
	p := s.Inspect()
	s.showChecks(out, p)
	if p.Scope == "" {
		return errors.New("cannot identify the local sandbox daemon scope")
	}
	var unlock func()
	var err error
	if explicit, _ := ctx.Value(explicitActionKey{}).(bool); explicit {
		unlock, err = waitManagementLock(ctx)
	} else {
		unlock, err = store.LockState()
	}
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.recoverBeforeRegistration(ctx, out, p, match == nil); err != nil {
		return err
	}
	if err := registrationPrerequisites(p.Checks); err != nil {
		return inventoryPrerequisiteError(p.Checks, err)
	}

	installation.Socket = p.Socket
	if installation.Scope != p.Scope {
		installation.AuthorizedID = ""
	}
	installation.Scope = p.Scope
	scan := reconciliation{service: s, ctx: ctx, out: out, prerequisites: p, installation: installation, match: match, current: current}
	if err := scan.load(); err != nil {
		return err
	}
	if err := scan.run(); err != nil {
		return err
	}
	var unavailable *InventoryUnavailableError
	if errors.As(scan.currentError, &unavailable) {
		return scan.currentError
	}
	return nil
}

func (s *Service) recoverBeforeRegistration(ctx context.Context, out io.Writer, p precheck.Result, full bool) error {
	if CheckAutomaticWork(context.Background()) != nil {
		return nil
	}
	if err := s.recoverPolicy(p.Scope, p.Socket); err != nil {
		return err
	}
	// Deletion recovery does not require inventory entries or guest SSH setup.
	if full && CheckAutomaticWork(context.Background()) == nil && hasLevel(p.Checks, "Herdr", precheck.Ready) && hasLevel(p.Checks, "Sandbox capability", precheck.Ready) && hasLevel(p.Checks, "Sandbox daemon", precheck.Ready) && !hasBlocked(p.Checks, "Docker authorization") {
		return s.recoverDeletedSandboxes(ctx, out, p.Scope, p.Socket)
	}
	return nil
}

func registrationPrerequisites(checks []precheck.Check) error {
	if hasBlocked(checks, "Sandbox capability") || hasBlocked(checks, "Sandbox daemon") || hasBlocked(checks, "Docker authorization") {
		return errors.New("fix the blocked prerequisites before registering sandboxes")
	}
	if !hasLevel(checks, "Sandbox capability", precheck.Ready) || !hasLevel(checks, "Sandbox daemon", precheck.Ready) || !hasLevel(checks, "Sandbox inventory", precheck.Ready) {
		return errors.New("complete the prerequisite checks before registering sandboxes")
	}
	return nil
}

type reconciliation struct {
	service        *Service
	ctx            context.Context
	out            io.Writer
	prerequisites  precheck.Result
	installation   installationOptions
	match, current func(sbx.Sandbox) bool
	currentError   error
	sandboxes      []sbx.Sandbox
	machines       []herdr.Machine
	state          store.State
	rows           map[string]SandboxRow
	tracked        map[string]store.Tracked
}

func (r *reconciliation) load() error {
	var err error
	r.sandboxes, err = r.service.inventory(r.ctx, r.prerequisites.Socket)
	if err != nil {
		return fmt.Errorf("list sandboxes: %w", &InventoryUnavailableError{Err: err})
	}
	if err := r.observeInventory(); err != nil {
		return err
	}
	beforeCatalog, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	var output string
	r.machines, output, err = r.service.herdr.ListMachines(r.ctx)
	if err != nil {
		return fmt.Errorf("list Herdr machines: %s", commandError(err, output))
	}
	r.state, err = store.LoadState()
	if err != nil {
		return err
	}
	if err := r.verifyAdoptions(); err != nil {
		return err
	}
	rows, err := (entryObservation{Scope: r.prerequisites.Scope, State: r.state, Complete: true, Revisions: []map[string]uint64{beforeCatalog.EnabledRevisions}, CreatedID: createdSandboxID(r.ctx), ImportNative: true}).entries(r.sandboxes, r.machines)
	if err != nil {
		return err
	}
	r.syncAssociations(rows)
	if err := store.SaveState(r.state); err != nil {
		return err
	}
	return r.resolveAbsentAddFailures()
}

func (r *reconciliation) observeInventory() error {
	for _, sb := range r.sandboxes {
		if sb.ID == "" {
			continue
		}
		if err := store.ObserveSandbox(r.prerequisites.Scope, sb); err != nil {
			return fmt.Errorf("save sandbox observation: %w", err)
		}
	}
	return nil
}

func (r *reconciliation) syncAssociations(rows []SandboxRow) {
	r.rows = map[string]SandboxRow{}
	for _, row := range rows {
		r.rows[row.Sandbox.ID] = row
		r.syncAssociation(row)
	}
	r.tracked = map[string]store.Tracked{}
	for _, item := range r.state.Scopes[r.prerequisites.Scope] {
		r.tracked[item.SandboxID] = item
	}
}

func (r *reconciliation) syncAssociation(row SandboxRow) {
	scope := r.prerequisites.Scope
	if !row.EntryPresent && row.AssociationIssue == "" {
		clearAbsentProfile(&r.state, scope, row.Sandbox.ID)
	}
	if !row.EntryPresent {
		return
	}
	item := store.Tracked{SandboxID: row.Sandbox.ID}
	for _, saved := range r.state.Scopes[scope] {
		if saved.SandboxID == row.Sandbox.ID {
			item = saved
			break
		}
	}
	if item.ProfileID != row.ProfileID {
		item.PendingLabel = ""
		item.ProjectWorkspaceDone = false
	}
	item.LastObservedNativeEnabled = row.NativeEnabled
	item.ProfileID, item.Target = row.ProfileID, row.Sandbox.Name+".sbx"
	item.Pending, item.Label, item.PendingProfileID = false, row.Label, ""
	if err := replaceTracked(&r.state, scope, item); err != nil {
		r.state.Scopes[scope] = append(r.state.Scopes[scope], item)
	}
}

func (r *reconciliation) run() error {
	var results []result
	resultSandboxes := map[string]sbx.Sandbox{}
	for _, sb := range r.sandboxes {
		outcome, err := r.process(sb)
		if err != nil {
			return err
		}
		if outcome != nil {
			results = append(results, *outcome)
			resultSandboxes[sb.Name] = sb
		}
	}
	if len(results) == 0 && r.service.printResult == nil {
		fmt.Fprintln(r.out, "No running sandboxes to register.")
	}
	var retryError error
	for _, outcome := range results {
		r.report(resultSandboxes[outcome.Name], outcome)
		if r.match != nil && outcome.RetryError != nil {
			retryError = outcome.RetryError
		}
	}
	return retryError
}

func (r *reconciliation) process(sb sbx.Sandbox) (*result, error) {
	if err := CheckAutomaticWork(r.ctx); err != nil {
		return nil, err
	}
	if r.match != nil && !r.match(sb) {
		return nil, nil
	}
	row := r.rows[sb.ID]
	explicit, _ := r.ctx.Value(explicitActionKey{}).(bool)
	if row.CreationRequested && createdSandboxID(r.ctx) != sb.ID && !explicit {
		return &result{Name: sb.Name, Status: "waiting for provisioning", Detail: row.Status.Detail}, nil
	}
	if row.ObservationPending {
		return nil, nil
	}
	if row.AssociationIssue != "" {
		return &result{Name: sb.Name, Status: "association warning", DiagnosticCode: store.CodeOwnershipConflict, Detail: row.AssociationIssue}, nil
	}
	if row.Enabled != store.Enabled {
		return r.disable(sb)
	}
	if !strings.EqualFold(sb.Status, "running") && (row.EntryPresent || !guestRuntimeAllowed(r.ctx, sb)) {
		return r.nonrunning(sb)
	}
	return r.running(sb)
}

func (r *reconciliation) disable(sb sbx.Sandbox) (*result, error) {
	item, known := r.tracked[sb.ID]
	if !known || item.ProfileID == "" {
		return nil, nil
	}
	m := associatedTrackedMachine(r.machines, item)
	if m == nil || (m.Enabled != nil && !*m.Enabled) {
		return nil, nil
	}
	if err := CheckAutomaticWork(r.ctx); err != nil {
		return nil, err
	}
	output, err := r.service.herdr.SetMachineEnabled(r.ctx, m.ID, false)
	after, _, readErr := r.service.herdr.ListMachines(r.ctx)
	confirmed := associatedTrackedMachine(after, item)
	if err != nil || readErr != nil || confirmed == nil || confirmed.Enabled == nil || *confirmed.Enabled {
		detail := "disabled herdr connection is not confirmed"
		if err != nil {
			detail += ": " + commandError(err, output)
		}
		return &result{Name: sb.Name, Status: "failed", Detail: detail}, nil
	}
	return nil, r.saveNativeChoice(item, confirmed.Enabled)
}

func (r *reconciliation) saveNativeChoice(item store.Tracked, enabled *bool) error {
	item.LastObservedNativeEnabled = enabled
	_ = replaceTracked(&r.state, r.prerequisites.Scope, item)
	return store.SaveState(r.state)
}

func (r *reconciliation) nonrunning(sb sbx.Sandbox) (*result, error) {
	saved, err := store.ReadStatusStore()
	if err != nil {
		return nil, err
	}
	if _, requested := saved.CreationRequests[store.StatusKey(r.prerequisites.Scope, sb.ID)]; requested {
		return &result{Name: sb.Name, Status: "waiting for provisioning", Detail: "Initial registration is pending. The plugin will retry automatically when this sandbox is ready."}, nil
	}

	if !strings.EqualFold(sb.Status, "stopped") {
		status, detail := deferredRuntime(sb.Status)
		return &result{Name: sb.Name, Status: status, Detail: detail}, nil
	}
	item, known := r.tracked[sb.ID]
	if known && item.ProfileID != "" {
		observed := store.SandboxStatus{}
		row := SandboxRow{Sandbox: sb, Enabled: store.Enabled, EntryPresent: true}
		if machine := associatedTrackedMachine(r.machines, item); machine != nil {
			row.NativeEnabled = machine.Enabled
		}
		applyObservedStatus(&observed, row)
		return &result{Name: sb.Name, Status: observed.Connection, Detail: observed.Detail}, nil
	}
	// Fail closed if the inventory changed after passive observation.
	if err := store.SetEnabled(r.prerequisites.Scope, sb.ID, store.Disabled); err != nil {
		return nil, err
	}
	return &result{Name: sb.Name, Status: "stopped", Detail: "This sandbox is stopped and has no herdr entry."}, nil
}

func (r *reconciliation) running(sb sbx.Sandbox) (*result, error) {
	if r.match != nil && !r.match(sb) {
		return nil, nil
	}
	if sb.ID == "" || sb.Name == "" {
		return &result{Name: sb.Name, Status: "failed", Detail: "sandbox has no stable identity"}, nil
	}
	if !hasLevel(r.prerequisites.Checks, "Sandbox SSH", precheck.Ready) {
		return &result{Name: sb.Name, Status: "setup required", Detail: "Set up sandbox SSH before preparing this running sandbox."}, nil
	}
	current := r.registrationCurrent(sb)
	item, known := r.tracked[sb.ID]
	if known && item.ProfileID != "" {
		return r.known(sb, item, current)
	}
	return r.untracked(sb, item, known, current)
}
func (r *reconciliation) untracked(sb sbx.Sandbox, item store.Tracked, known bool, current func() bool) (*result, error) {
	if known && item.Pending {
		if pendingCollision(r.machines, item, sb) {
			return &result{Name: sb.Name, Status: "association warning", Detail: "Another saved Herdr connection uses this sandbox's name or address. Registration is paused to avoid changing it."}, nil
		}
		return &result{Name: sb.Name, Status: "registration unconfirmed", DiagnosticCode: store.CodeRegistration, Detail: "Herdr has no saved connection to this sandbox. The plugin will check again automatically."}, nil
	}

	return r.register(sb, current), nil
}

func (r *reconciliation) registrationCurrent(sb sbx.Sandbox) func() bool {
	return func() bool {
		r.currentError = CheckAutomaticWork(r.ctx)
		if r.currentError != nil {
			return false
		}
		if !sandboxWorkAllowed(r.ctx, r.prerequisites.Scope, sb.ID) {
			r.currentError = fmt.Errorf("Sandbox work is disabled or its current choice could not be verified")
			return false
		}
		r.currentError = r.service.sandboxCurrent(r.ctx, r.prerequisites.Socket, sb)
		if r.currentError != nil {
			return false
		}
		if r.current != nil && !r.current(sb) {
			r.currentError = fmt.Errorf("Sandbox lifecycle authorization is no longer current")
			return false
		}
		return true
	}
}

func (r *reconciliation) known(sb sbx.Sandbox, item store.Tracked, current func() bool) (*result, error) {
	outcome, err := r.enable(sb, &item, current)
	if err != nil || outcome != nil {
		return outcome, err
	}
	if err := r.service.recoverMachineLabel(r.ctx, registrationWork{Scope: r.prerequisites.Scope, State: &r.state, Sandbox: sb, Current: current}, r.machines, &item); err != nil {
		return &result{Name: sb.Name, Status: "association warning", Detail: "saved machine label was not updated: " + err.Error()}, nil
	}
	value := r.service.projectReadiness(r.ctx, registrationWork{Scope: r.prerequisites.Scope, State: &r.state, Sandbox: sb, Current: current}, &item, true)
	return &value, nil
}

func (r *reconciliation) enable(sb sbx.Sandbox, item *store.Tracked, current func() bool) (*result, error) {
	machine := associatedTrackedMachine(r.machines, *item)
	if machine == nil || machine.Enabled == nil || *machine.Enabled {
		return nil, nil
	}
	if !current() {
		return nil, context.Canceled
	}
	output, err := r.service.herdr.SetMachineEnabled(r.ctx, machine.ID, true)
	if err != nil {
		return &result{Name: sb.Name, Status: "failed", Detail: "enable herdr connection: " + commandError(err, output)}, nil
	}
	after, _, err := r.service.herdr.ListMachines(r.ctx)
	confirmed := associatedTrackedMachine(after, *item)
	if err != nil || confirmed == nil || confirmed.Enabled == nil || !*confirmed.Enabled {
		return &result{Name: sb.Name, Status: "failed", Detail: "herdr connection enable is not confirmed"}, nil
	}
	item.LastObservedNativeEnabled = confirmed.Enabled
	return nil, r.saveNativeChoice(*item, confirmed.Enabled)
}

func pendingCollision(machines []herdr.Machine, item store.Tracked, sb sbx.Sandbox) bool {
	for _, machine := range machines {
		if machine.Label == item.Label || machine.Target == item.Target || machine.Label == sandboxMachineLabel(sb.Name) || machine.Target == sb.Name+".sbx" {
			return true
		}
	}
	return false
}

func (r *reconciliation) register(sb sbx.Sandbox, current func() bool) *result {
	var progressStatusErr error
	outcome, saved := r.service.registerWithInstallation(r.ctx, registrationWork{Scope: r.prerequisites.Scope, State: &r.state, Sandbox: sb, Current: current, CurrentError: func() error { return r.currentError }}, r.machines, r.installation, func(message string) {
		if err := store.RecordSandboxStatus(r.prerequisites.Scope, sb, "preparing", message, false); err != nil {
			progressStatusErr = err
		}
		fmt.Fprintf(r.out, "%s: %s\n", sb.Name, message)
	})
	if progressStatusErr != nil {
		fmt.Fprintln(r.out, "Sandbox status was not saved:", progressStatusErr)
	}
	if saved != nil {
		r.tracked[sb.ID] = *saved
	}
	return &outcome
}

func (r *reconciliation) report(sb sbx.Sandbox, item result) {
	if sb.ID != "" {
		if err := store.RecordSandboxStatus(r.prerequisites.Scope, sb, item.Status, item.Detail, false); err != nil {
			fmt.Fprintln(r.out, "Sandbox status was not saved:", err)
		}
	}
	if err := saveResultError(store.StatusKey(r.prerequisites.Scope, sb.ID), item); err != nil {
		fmt.Fprintln(r.out, "Sandbox diagnostic was not saved:", err)
	} else {
		r.service.notifyResult(r.ctx, r.out, r.prerequisites.Scope, sb.ID, item)
	}
	if r.service.printResult != nil {
		r.service.printResult(r.out, ResultObservation{SandboxID: sb.ID, Name: item.Name, Status: item.Status, Detail: item.Detail, DiagnosticCode: resultDiagnosticCode(item)})
	} else {
		fmt.Fprintf(r.out, "%-24s %-18s %s\n", item.Name, strings.ToUpper(item.Status), item.Detail)
	}
}

func (s *Service) registerPrepared(scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox) (result, *store.Tracked) {
	return s.registerPreparedContext(context.Background(), registrationWork{Scope: scope, State: st, Sandbox: sb, Current: nil}, machines)
}

func (s *Service) registerPreparedContext(ctx context.Context, work registrationWork, machines []herdr.Machine) (result, *store.Tracked) {
	return s.registerPreparedWithProgress(ctx, work, machines, nil)
}

func (s *Service) registerPreparedWithProgress(ctx context.Context, work registrationWork, machines []herdr.Machine, progress func(string)) (result, *store.Tracked) {
	return s.registerWithInstallation(ctx, work, machines, installationOptions{}, progress)
}

func (s *Service) registerWithInstallation(ctx context.Context, work registrationWork, machines []herdr.Machine, installation installationOptions, progress func(string)) (result, *store.Tracked) {
	scope, st, sb, current := work.Scope, work.State, work.Sandbox, work.Current
	ctx = context.WithValue(ctx, registrationIdentityKey{}, registrationIdentity{Scope: scope, Sandbox: sb})
	attempt := registrationAttempt{service: s, ctx: ctx, scope: scope, state: st, machines: machines, sandbox: sb, current: current, currentError: work.CurrentError, installation: installation, progress: progress, target: sb.Name + ".sbx"}
	if outcome := attempt.collision(); outcome != nil {
		return *outcome, nil
	}
	if outcome := attempt.prepareHerdr(); outcome != nil {
		return *outcome, nil
	}
	if outcome := attempt.saveIntent(); outcome != nil {
		return *outcome, nil
	}
	return attempt.add()
}

type registrationAttempt struct {
	service       *Service
	ctx           context.Context
	scope         string
	state         *store.State
	machines      []herdr.Machine
	sandbox       sbx.Sandbox
	current       func() bool
	currentError  func() error
	installation  installationOptions
	progress      func(string)
	target        string
	pending       store.Tracked
	catalogAbsent bool
	repairRetried bool
}

func (a *registrationAttempt) collision() *result {
	if a.current != nil && !a.current() {
		value := a.currentResult("sandbox changed before registration")
		return &value
	}
	for _, m := range a.machines {
		if m.Label == sandboxMachineLabel(a.sandbox.Name) || m.Target == a.target {
			if m.Target == a.target {
				return &result{Name: a.sandbox.Name, Status: "association warning", Detail: "A herdr entry already exists for this sandbox; refresh its association before creating an entry."}
			}
			return &result{Name: a.sandbox.Name, Status: "association warning", Detail: "Another herdr entry already uses this label. Rename that entry before creating one for this sandbox."}
		}
	}

	return nil
}

func (a *registrationAttempt) prepareHerdr() *result {
	remote, err := a.service.remoteHerdrStatus(a.ctx, a.target)
	if err != nil {
		return a.probeFailure(err)
	}
	versionOutput := remote.Version
	if remote.Missing {
		prepared, outcome := a.prepareMissingHerdr()
		if outcome != nil {
			return outcome
		}
		versionOutput = prepared

	} else if remote.Conflict {
		return &result{Name: a.sandbox.Name, Status: "setup required", Detail: "remote Herdr path exists but is not executable; it was left unchanged"}
	}
	if installedVersion := version.Parse(versionOutput); installedVersion == "" || version.Compare(installedVersion, minHerdrVersion) < 0 {
		return &result{Name: a.sandbox.Name, Status: "setup required", Detail: "remote Herdr is missing or too old"}
	}
	if a.current != nil && !a.current() {
		value := a.currentResult("sandbox changed before registration")
		return &value
	}

	if err := a.service.prepareGuestShell(a.ctx, a.sandbox, a.current); err != nil {
		return guestShellFailure(a.sandbox.Name, err)
	}
	return nil
}

func (a *registrationAttempt) probeFailure(err error) *result {
	a.service.recordGuestServerObservation(a.ctx, a.scope, a.sandbox, false, "")
	outcome := &result{Name: a.sandbox.Name, Status: "connection failed", DiagnosticCode: store.CodeSandboxSetup, Detail: "remote Herdr is unavailable: " + err.Error()}
	// No add intent exists yet; retaining a creation event cannot replay an add.
	if a.pending.SandboxID == "" && a.ctx.Err() == nil && transientAddFailure(err.Error(), err) {
		outcome.RetryError = &PreAddUnavailableError{Err: err}
	}
	return outcome
}

func (a *registrationAttempt) prepareMissingHerdr() (string, *result) {
	preparationKey := a.scope + "/" + a.sandbox.ID
	allowed, cooldownErr := store.PreparationAllowed(preparationKey)
	if cooldownErr != nil {
		return "", &result{Name: a.sandbox.Name, Status: "failed", Detail: "preparation status is unavailable: " + cooldownErr.Error()}
	}
	if !allowed {
		detail, err := store.PreparationCooldownDetail(preparationKey)
		if err != nil {
			return "", &result{Name: a.sandbox.Name, Status: "failed", Detail: "cannot read last preparation failure: " + err.Error()}
		}
		return "", &result{Name: a.sandbox.Name, Status: "setup required", Detail: detail}
	}
	versionOutput, err := a.service.installHerdr(a.ctx, installationRequest{Scope: a.scope, Socket: a.installation.Socket, Sandbox: a.sandbox, Authorized: a.installation.Scope == a.scope && a.installation.AuthorizedID == a.sandbox.ID, Current: a.current, Progress: a.progress})
	if err != nil {
		return "", a.preparationFailure(preparationKey, err)
	}

	if cooldownErr := store.ClearPreparationFailure(preparationKey); cooldownErr != nil {
		return "", &result{Name: a.sandbox.Name, Status: "failed", Detail: "preparation completed but its retry state was not cleared: " + cooldownErr.Error()}
	}
	return versionOutput, nil
}

func (a *registrationAttempt) preparationFailure(preparationKey string, err error) *result {
	if cooldownErr := store.NotePreparationFailure(preparationKey, err.Error(), a.service.preparationRetryDelay); cooldownErr != nil {
		return &result{Name: a.sandbox.Name, Status: "failed", Detail: "preparation failed and its retry state was not saved: " + cooldownErr.Error()}
	}
	if errors.Is(err, context.Canceled) {
		return &result{Name: a.sandbox.Name, Status: "stale", Detail: "sandbox changed while Herdr was being prepared"}
	}
	var outcome *installationError
	if errors.As(err, &outcome) {
		return &result{Name: a.sandbox.Name, Status: outcome.State, Detail: outcome.Detail}
	}
	return &result{Name: a.sandbox.Name, Status: "setup required", Detail: "remote Herdr preparation failed: " + err.Error()}
}

func (a *registrationAttempt) saveIntent() *result {

	initialCWD, err := a.service.prepareInitialProjectWorkspace(a.ctx, a.scope, a.state, a.sandbox, a.current)
	if err != nil {
		return &result{Name: a.sandbox.Name, Status: "workspace setup required", Detail: err.Error()}
	}
	a.pending = store.Tracked{SandboxID: a.sandbox.ID, Label: sandboxMachineLabel(a.sandbox.Name), Target: a.target, Pending: true, ProjectWorkspacePending: initialCWD}
	a.preserveCompletedWork()
	if err := replaceTracked(a.state, a.scope, a.pending); err != nil {
		a.state.Scopes[a.scope] = append(a.state.Scopes[a.scope], a.pending)
	}
	if err := store.SaveState(*a.state); err != nil {
		return &result{Name: a.sandbox.Name, Status: "failed", Detail: err.Error()}
	}
	return nil
}

func (a *registrationAttempt) preserveCompletedWork() {
	for _, previous := range a.state.Scopes[a.scope] {
		if previous.SandboxID == a.sandbox.ID {
			a.pending.ProjectWorkspaceDone = previous.ProjectWorkspaceDone
			a.pending.GuestSync = previous.GuestSync
			break
		}
	}
}

func (a *registrationAttempt) add() (result, *store.Tracked) {

	if a.current != nil && !a.current() {
		if err := clearUnperformedAdd(a.state, a.scope, a.pending.SandboxID); err != nil || store.SaveState(*a.state) != nil {
			return result{Name: a.sandbox.Name, Status: "association warning", Detail: "sandbox changed and registration intent could not be cleared"}, &a.pending
		}
		return a.currentResult("sandbox changed before machine registration"), nil
	}

	return a.addCurrent()
}

// Keep pre-add permission until the last identity guard has allowed the command.
func (a *registrationAttempt) handoffCreation() *result {
	requireRequest := createdSandboxID(a.ctx) == a.sandbox.ID
	allowed, err := store.HandoffCreationRequest(a.scope, a.sandbox.ID, a.sandbox.Name, requireRequest)
	if err == nil && allowed {
		return nil
	}
	clearErr := clearUnperformedAdd(a.state, a.scope, a.sandbox.ID)
	saveErr := store.SaveState(*a.state)
	if failure := errors.Join(err, clearErr, saveErr); failure != nil {
		return &result{Name: a.sandbox.Name, Status: "failed", Detail: "save registration handoff: " + failure.Error()}
	}
	return &result{Name: a.sandbox.Name, Status: "disabled", Detail: "Initial registration was cancelled."}
}

func (a *registrationAttempt) addCurrent() (result, *store.Tracked) {
	if outcome := a.handoffCreation(); outcome != nil {
		return *outcome, nil
	}
	started := time.Now()
	profileID, addOutput, addErr := a.service.herdr.AddMachine(a.ctx, a.pending.Label, a.target)
	a.service.logRegistration(a.ctx, registrationLog{Stage: "machine_add", Scope: a.scope, Sandbox: a.sandbox, Attempt: 1 + boolInt(a.repairRetried), Started: started, Timeout: 30 * time.Second, Ack: profileID, Err: addErr, Output: addOutput})
	a.pending.PendingProfileID = profileID
	if err := saveTracked(a.state, a.scope, a.pending); err != nil {
		return result{Name: a.sandbox.Name, Status: "association warning", Detail: "Machine add finished, but its acknowledged ID could not be saved: " + err.Error()}, &a.pending
	}
	if outcome := a.verifyAddedEntry(); outcome != nil {
		return a.unverifiedAdd(*outcome, addOutput, addErr)
	}

	a.pending.ProfileID = profileID
	a.pending.Pending, a.pending.PendingProfileID = false, ""
	if err := saveTracked(a.state, a.scope, a.pending); err != nil {
		return result{Name: a.sandbox.Name, Status: "association warning", Detail: "machine was registered but association could not be saved"}, &a.pending
	}
	if err := store.ClearCreationRequest(a.scope, a.sandbox.ID); err != nil {
		return result{Name: a.sandbox.Name, Status: "association warning", Detail: "The saved connection was verified, but its completed registration request could not be cleared: " + err.Error()}, &a.pending
	}
	if a.current != nil && !a.current() {
		return a.currentResult("sandbox changed after machine registration; cleanup is pending"), &a.pending
	}
	return a.service.projectReadiness(a.ctx, registrationWork{Scope: a.scope, State: a.state, Sandbox: a.sandbox, Current: a.current}, &a.pending, false), &a.pending
}

func (a *registrationAttempt) unverifiedAdd(outcome result, output string, err error) (result, *store.Tracked) {
	if err == nil {
		if a.catalogAbsent {
			outcome.Detail = fmt.Sprintf("Herdr reported success, but no connection was saved after %d checks.", addVerificationAttempts)
		}
		return outcome, &a.pending
	}
	if a.catalogAbsent && !a.repairRetried && a.pending.PendingProfileID == "" {
		if strings.Contains(output, "remote server is not ready for saved machines") {
			return a.retryAfterRepair()
		}
		if transientAddFailure(output, err) {
			return a.retryTransient()
		}
	}
	if settled := a.settleKnownUnsaved(output, err); settled != nil {
		return *settled, nil
	}
	failure, _ := a.addFailure(output, err)
	outcome.Detail = failure.Detail + ". " + outcome.Detail
	if failure.Status == "catalog full" {
		outcome.Status = failure.Status
	}
	return outcome, &a.pending
}

func (a *registrationAttempt) addFailure(addOutput string, addErr error) (result, *store.Tracked) {

	if deterministicAddFailure(addOutput) {
		return addFailureResult(a.sandbox.Name, addErr, addOutput), &a.pending
	}

	return result{Name: a.sandbox.Name, Status: "association warning", Detail: "Herdr could not save the connection: " + commandError(addErr, addOutput)}, &a.pending
}

func (s *Service) sandboxRunning(socket string, expected sbx.Sandbox) bool {
	return s.sandboxCurrent(context.Background(), socket, expected) == nil
}

func addFailureResult(name string, err error, output string) result {
	detail := commandError(err, output)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "capacity") || strings.Contains(lower, "catalog full") || strings.Contains(lower, "maximum") || strings.Contains(lower, "limit") {
		return result{Name: name, Status: "catalog full", Detail: "Herdr cannot save another machine: " + detail + ". Remove an unused machine, then retry."}
	}
	return result{Name: name, Status: "failed", Detail: "machine add failed: " + detail + ". Retry after correcting the reported problem."}
}

func replaceTracked(st *store.State, scope string, next store.Tracked) error {
	for i, item := range st.Scopes[scope] {
		if item.SandboxID == next.SandboxID {
			st.Scopes[scope][i] = next
			return nil
		}
	}
	return errors.New("registration intent is missing")
}

// clearUnperformedAdd discards a creation request without discarding completed
// guest work. Fresh requests without guest effects need no retained record.
func clearUnperformedAdd(st *store.State, scope, id string) error {
	for i := range st.Scopes[scope] {
		item := &st.Scopes[scope][i]
		if item.SandboxID != id {
			continue
		}
		if !item.ProjectWorkspaceDone && item.GuestSync == nil && item.ProjectWorkspacePending == "" {
			return removeTracked(st, scope, id)
		}
		item.ProfileID = ""
		item.Pending = false
		item.PendingLabel = ""
		item.LastObservedNativeEnabled = nil
		return nil
	}
	return errors.New("registration intent is missing")
}

func removeTracked(st *store.State, scope, sandboxID string) error {
	for i, item := range st.Scopes[scope] {
		if item.SandboxID == sandboxID {
			st.Scopes[scope] = append(st.Scopes[scope][:i], st.Scopes[scope][i+1:]...)
			return nil
		}
	}
	return errors.New("registration intent is missing")
}
func hasBlocked(checks []precheck.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name && c.Level == precheck.Blocked {
			return true
		}
	}
	return false
}

func hasLevel(checks []precheck.Check, name string, expected precheck.Level) bool {
	for _, c := range checks {
		if c.Name == name && c.Level == expected {
			return true
		}
	}
	return false
}

func commandError(err error, output string) string {
	var upstream *herdr.CommandError
	if errors.As(err, &upstream) {
		return upstream.Error()
	}
	message := strings.TrimSpace(output)
	if message == "" {
		return err.Error()
	}
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 180 {
		message = message[:177] + "..."
	}
	return message
}

func deterministicAddFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "capacity") || strings.Contains(lower, "catalog full") || strings.Contains(lower, "maximum") || strings.Contains(lower, "limit")
}

// Save the diagnostic before advertising its reference in a notification.
func saveResultError(scope string, item result) error {
	if item.Status == "ready" {
		if err := store.SetRegistrationError("", scope, ""); err != nil {
			return err
		}
		return store.ClearGuestPluginFailure(scope)
	}
	if item.Status == "stale" || item.Status == "retrying" {
		return nil
	}
	if resultDiagnosticCode(item) == "" {
		return store.SetRegistrationError("", scope, "")
	}
	if item.Status == "plugin setup required" || item.Status == "manual intervention required" {
		return savePluginSetupError(scope, item)
	}
	code := resultDiagnosticCode(item)
	return store.SetRegistrationError(code, scope, item.Detail)
}

// resultDiagnosticCode classifies the actual reconciliation outcome for both
// saved diagnostics and non-table result presentations.
func resultDiagnosticCode(item result) string {
	if item.Status == "ready" || item.Status == "stale" || item.Status == "retrying" {
		return ""
	}
	if item.DiagnosticCode != "" {
		return item.DiagnosticCode
	}
	switch item.Status {
	case "stopped", "waiting for provisioning", "waiting for start", "disabled in herdr", "enable not confirmed", "disabled", "pending", "runtime unknown", "runtime failed":
		return ""
	case "association warning":
		return store.CodeOwnershipConflict
	case "workspace setup required":
		return store.CodeWorkspaceSetup
	case "setup required", "connection failed", "preparation failed":
		return store.CodeSandboxSetup
	case "preparation timed out":
		return store.CodeGuestShellTimeout
	case "plugin setup required", "manual intervention required":
		return store.CodeGuestPluginSync
	default:
		return store.CodeRegistration
	}
}

func savePluginSetupError(scope string, item result) error {
	if err := store.SetRegistrationError("", scope, ""); err != nil {
		return err
	}
	if len(item.PluginFailures) == 0 {
		// Catalog/defaults could not be read. No plugin recovery was observed.
		return store.RecordGuestPluginFailureCode(store.CodeGuestPluginSync, scope, item.Detail)
	}
	return store.SetGuestPluginFailures(scope, item.PluginFailures)
}
