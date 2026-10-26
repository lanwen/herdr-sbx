package registration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// SandboxRow is a passive inventory/catalog observation for the manager.
type SandboxRow struct {
	Sandbox            sbx.Sandbox
	Scope              string
	Enabled            store.EnabledChoice
	EntryPresent       bool
	NativeEnabled      *bool
	AssociationIssue   string
	ObservationPending bool
	Label, ProfileID   string
	Status             store.SandboxStatus
}

// SandboxRows observes inventory and registrations without connecting to guests.
// Newly discovered identities default to Enabled; labels do not establish identity.
func (s *Service) SandboxRows(ctx context.Context, socket string) ([]SandboxRow, error) {
	if socket == "" {
		return nil, errors.New("sandbox daemon socket is unavailable")
	}
	scope := store.ScopeID(socket)
	sandboxes, err := s.inventory(ctx, socket)
	if err != nil {
		_ = store.MarkScopeStale(scope, err.Error())
		return nil, err
	}
	beforeCatalog, err := store.ReadStatusStore()
	if err != nil {
		return nil, err
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		detail := "herdr catalog is unavailable; labels and connection state are last known observations."
		_ = store.MarkScopeStale(scope, detail)
		status, readErr := store.ReadStatusStore()
		if readErr != nil {
			return nil, readErr
		}
		rows := cachedSandboxRows(scope, sandboxes, status, detail)
		return rows, fmt.Errorf("read herdr catalog: %s", commandError(err, output))
	}
	state, err := store.LoadState()
	if err != nil {
		return nil, err
	}
	return observeEntries(scope, sandboxes, machines, state, true, beforeCatalog.EnabledRevisions)
}

// associatedMachine verifies immutable catalog identity; labels never establish identity.
func associatedMachine(scope string, sb sbx.Sandbox, machines []herdr.Machine, state store.State) (*herdr.Machine, string) {
	target := sb.Name + ".sbx"
	if sb.ID == "" || sb.Name == "" || SandboxName(store.Tracked{Target: target}) != sb.Name {
		return nil, "Sandbox identity is unavailable."
	}
	saved, issue := savedSandboxAssociation(scope, sb.ID, target, state)
	if issue != "" {
		return nil, issue
	}
	candidates := machinesForTarget(machines, target)
	if len(candidates) > 1 {
		return nil, "More than one herdr entry uses this sandbox target."
	}
	if len(candidates) == 0 {
		return absentAssociatedMachine(saved, machines)
	}
	m := candidates[0]
	if m.ID == "" || !usesDefaultSession(m.Session) {
		return nil, "The herdr entry has an unknown identity or another session."
	}
	if savedAssociationChanged(saved, m, target) {
		return nil, "The saved herdr entry identity changed. Refresh and inspect the association."
	}
	if machineClaimedElsewhere(scope, sb.ID, m.ID, state) {
		return nil, "This herdr entry is associated with another sandbox."
	}
	return &m, ""
}
func savedSandboxAssociation(scope, id, target string, state store.State) (*store.Tracked, string) {
	var saved *store.Tracked
	for otherScope, entries := range state.Scopes {
		var issue string
		saved, issue = inspectSavedAssociations(scope, otherScope, id, target, entries, saved)
		if issue != "" {
			return nil, issue
		}
	}
	return saved, ""
}
func inspectSavedAssociations(scope, otherScope, id, target string, entries []store.Tracked, saved *store.Tracked) (*store.Tracked, string) {
	for i := range entries {
		entry := &entries[i]
		if otherScope != scope || entry.SandboxID != id {
			if entry.Target == target {
				return nil, "This SSH target belongs to another saved sandbox identity."
			}
			continue
		}
		if saved != nil {
			return nil, "More than one saved association identifies this sandbox."
		}
		saved = entry
	}
	return saved, ""
}

func machinesForTarget(machines []herdr.Machine, target string) []herdr.Machine {
	var candidates []herdr.Machine
	for _, m := range machines {
		if m.Target == target {
			candidates = append(candidates, m)
		}
	}
	return candidates
}
func absentAssociatedMachine(saved *store.Tracked, machines []herdr.Machine) (*herdr.Machine, string) {
	if saved != nil && saved.ProfileID != "" && machineByID(machines, saved.ProfileID) != nil {
		return nil, "The saved herdr entry now uses another target; inspect its association."
	}
	return nil, ""
}
func machineClaimedElsewhere(scope, id, profileID string, state store.State) bool {
	for otherScope, entries := range state.Scopes {
		for _, entry := range entries {
			if entry.ProfileID == profileID && (otherScope != scope || entry.SandboxID != id) {
				return true
			}
		}
	}
	return false
}

func observeManagement(scope string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State) ([]SandboxRow, error) {
	return observeEntries(scope, sandboxes, machines, state, true)
}
func observeEntries(scope string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State, complete bool, observedRevisions ...map[string]uint64) ([]SandboxRow, error) {
	var rows []SandboxRow
	err := store.UpdateStatusStore(func(status *store.StatusStore) {
		markMissingInventory(status, scope, sandboxes, complete)
		for _, sb := range sandboxes {
			if sb.ID != "" {
				rows = append(rows, observeSandboxEntry(status, scope, sb, machines, state, observedRevisions))
			}
		}
	})
	return rows, err
}
func markMissingInventory(status *store.StatusStore, scope string, sandboxes []sbx.Sandbox, complete bool) {
	present := map[string]bool{}
	for _, sb := range sandboxes {
		present[sb.ID] = true
	}
	for key, cached := range status.Sandboxes {
		if complete && cached.Scope == scope && !present[cached.SandboxID] {
			cached.Stale = true
			cached.Detail = "not present in latest inventory; deletion is unconfirmed"
			status.Sandboxes[key] = cached
		}
	}
}
func observeSandboxEntry(status *store.StatusStore, scope string, sb sbx.Sandbox, machines []herdr.Machine, state store.State, revisions []map[string]uint64) SandboxRow {
	key := store.StatusKey(scope, sb.ID)
	choice := status.EnabledChoices[key]
	if choice == "" {
		choice = store.Enabled
		status.EnabledChoices[key] = choice
	}
	if len(revisions) > 0 && revisions[0][key] != status.EnabledRevisions[key] {
		return supersededSandboxRow(status, scope, sb, choice)
	}
	row := SandboxRow{Scope: scope, Sandbox: sb, Enabled: choice}
	machine, issue := associatedMachine(scope, sb, machines, state)
	row.AssociationIssue = issue
	row.Enabled = settleObservedEnabled(status, key, choice, machine, issue, revisions)
	row.ObservationPending = status.EnabledObservationPending[key]
	if issue == "" && machine == nil && strings.EqualFold(sb.Status, "stopped") {
		row.Enabled = store.Disabled
		status.EnabledChoices[key] = row.Enabled
	}
	collision := entryLabelCollision(row, machine, machines)
	clearAssociationDiagnostics(status, key, issue, collision)
	if machine != nil {
		row.EntryPresent = true
		row.Label = machine.Label
		row.ProfileID = machine.ID
		row.NativeEnabled = machine.Enabled
	}
	row.Status = cacheObservedEntry(status, key, row, collision)
	return row
}
func supersededSandboxRow(status *store.StatusStore, scope string, sb sbx.Sandbox, choice store.EnabledChoice) SandboxRow {
	cached := status.Sandboxes[store.StatusKey(scope, sb.ID)]
	cached.Enabled, cached.Stale = choice, true
	return SandboxRow{Scope: scope, Sandbox: sb, Enabled: choice, EntryPresent: cached.EntryPresent, NativeEnabled: cached.NativeEnabled, Label: cached.Label, ProfileID: cached.ProfileID, AssociationIssue: cached.AssociationIssue, ObservationPending: true, Status: cached}
}
func settleObservedEnabled(status *store.StatusStore, key string, choice store.EnabledChoice, machine *herdr.Machine, issue string, revisions []map[string]uint64) store.EnabledChoice {
	fresh := false
	if len(revisions) > 0 {
		before, known := revisions[0][key]
		fresh = known && before == status.EnabledRevisions[key]
	}
	if !status.EnabledObservationPending[key] || !fresh || issue != "" || (machine != nil && machine.Enabled == nil) {
		return choice
	}
	choice = store.Disabled
	if machine != nil && *machine.Enabled {
		choice = store.Enabled
	}
	status.EnabledChoices[key] = choice
	status.EnabledRevisions[key]++
	delete(status.EnabledObservationPending, key)
	return choice
}
func entryLabelCollision(row SandboxRow, machine *herdr.Machine, machines []herdr.Machine) bool {
	if row.AssociationIssue != "" || machine != nil || row.Enabled != store.Enabled || !strings.EqualFold(row.Sandbox.Status, "running") {
		return false
	}
	for _, candidate := range machines {
		if candidate.Label == sandboxMachineLabel(row.Sandbox.Name) {
			return true
		}
	}
	return false
}
func clearAssociationDiagnostics(status *store.StatusStore, key, issue string, collision bool) {
	if issue != "" {
		return
	}
	for diagnosticKey, diagnostic := range status.Errors {
		if diagnostic.Scope == key && (diagnostic.Code == store.CodeOwnershipConflict && !collision || diagnostic.Code == store.CodeSafeEntryUnsupported) {
			delete(status.Errors, diagnosticKey)
		}
	}
}
func cacheObservedEntry(status *store.StatusStore, key string, row SandboxRow, collision bool) store.SandboxStatus {
	cached := status.Sandboxes[key]
	cached.Scope, cached.SandboxID, cached.Name, cached.Observed = row.Scope, row.Sandbox.ID, row.Sandbox.Name, row.Sandbox.Status
	cached.Enabled, cached.EntryPresent, cached.NativeEnabled, cached.AssociationIssue = row.Enabled, row.EntryPresent, row.NativeEnabled, row.AssociationIssue
	cached.Label, cached.ProfileID = row.Label, row.ProfileID
	cached.ObservedAt = time.Now()
	cached.ConfirmPresent()
	applyObservedStatus(&cached, row)
	if collision {
		cached.Connection = "label already in use"
		cached.Detail = "Another herdr entry already uses this label. Rename that entry before creating one for this sandbox."
	}
	status.SetSandboxDeletionFailure(key, "")
	if row.ObservationPending {
		cached.Stale = true
		cached.Connection = "observation unavailable"
		cached.Detail = "The herdr connection change could not be verified. Automatic work waits for a fresh observation."
	}
	status.Sandboxes[key] = cached
	return cached
}

func (s *Service) CaptureBaseline(ctx context.Context, socket string) error {
	if socket == "" {
		return errors.New("sandbox daemon socket is unavailable")
	}
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	scope := store.ScopeID(socket)
	initial, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	sandboxes, err := s.inventory(ctx, socket)
	if err != nil {
		return err
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return fmt.Errorf("read herdr baseline: %s", commandError(err, output))
	}
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	for _, sb := range sandboxes {
		if err := captureSandboxBaseline(scope, sb, machines, initial, &state); err != nil {
			return err
		}
	}
	if err := store.SaveState(state); err != nil {
		return err
	}
	_, err = observeManagement(scope, sandboxes, machines, state)
	return err
}

// SetEnabled changes durable intent and the native connection without removing an entry.
// enabledChange owns one accepted connection command through observation and settlement.
type enabledChange struct {
	service   *Service
	ctx       context.Context
	scope, id string
	choice    store.EnabledChoice
	state     store.State
	sb        sbx.Sandbox
	machines  []herdr.Machine
	machine   *herdr.Machine
	observed  bool
	unlock    func()
	revision  uint64
	confirmed store.EnabledChoice
	pending   bool
}

func (s *Service) SetEnabled(ctx context.Context, scope, id string, choice store.EnabledChoice) (returnErr error) {
	ctx = WithExplicitAction(ctx)
	if choice != store.Enabled && choice != store.Disabled {
		return errors.New("invalid enabled choice")
	}
	revision, confirmed, pending, err := store.BeginEnabledChange(scope, id, choice)
	if err != nil {
		return err
	}
	change := &enabledChange{service: s, ctx: ctx, scope: scope, id: id, choice: choice, revision: revision, confirmed: confirmed, pending: pending}
	defer func() { returnErr = errors.Join(returnErr, change.settle()) }()
	return change.run()
}
func (c *enabledChange) settle() error {
	if c.unlock != nil {
		defer c.unlock()
	}
	applied, settleErr := store.SettleEnabledChoice(c.scope, c.id, c.confirmed, c.revision, c.pending)
	if !applied || !c.observed {
		return settleErr
	}
	_, observeErr := observeEntries(c.scope, []sbx.Sandbox{c.sb}, c.machines, c.state, false)
	return errors.Join(settleErr, observeErr)
}
func (c *enabledChange) commandCurrent() bool {
	saved, err := store.ReadStatusStore()
	return err == nil && saved.EnabledRevisions[store.StatusKey(c.scope, c.id)] == c.revision
}
func supersededEnabledChange() error {
	return fmt.Errorf("a newer sandbox choice replaced this action: %w", context.Canceled)
}
func (c *enabledChange) run() error {
	if err := c.prepare(); err != nil {
		return err
	}
	if c.machine == nil {
		return c.addEntry()
	}
	if c.choice == store.Enabled {
		if err := c.confirmEnableRuntime(); err != nil {
			return err
		}
	}
	want := c.choice == store.Enabled
	if c.machine.Enabled == nil || *c.machine.Enabled != want {
		if err := c.mutateNativeEnabled(want); err != nil {
			return err
		}
	}
	if !c.commandCurrent() {
		_, _ = observeEntries(c.scope, []sbx.Sandbox{c.sb}, c.machines, c.state, false)
		return supersededEnabledChange()
	}
	return c.saveEntry()
}
func (c *enabledChange) observeBeforeChange() error {
	c.observed = true
	if c.machine == nil {
		c.confirmed, c.pending = store.Disabled, false
	} else if c.machine.Enabled != nil {
		c.confirmed, c.pending = store.Disabled, false
		if *c.machine.Enabled {
			c.confirmed = store.Enabled
		}
	}
	// Keep a last-confirmed entry observation if the mutation outcome is lost.
	if _, err := observeEntries(c.scope, []sbx.Sandbox{c.sb}, c.machines, c.state, false); err != nil {
		return err
	}

	return nil
}
func (c *enabledChange) addEntry() error {
	if c.choice == store.Disabled {
		return nil
	}
	if !strings.EqualFold(c.sb.Status, "running") {
		if strings.EqualFold(c.sb.Status, "stopped") {
			return nil
		}
		status, detail := deferredRuntime(c.sb.Status)
		_ = store.RecordSandboxStatus(c.scope, c.sb, status, detail, false)
		return errors.New("Sandbox runtime is not confirmed running. Refresh before enabling it.")
	}
	// Ordinary add and guest setup are permitted only after rechecking RUNNING.
	current := func() bool {
		return c.service.sandboxRunning(c.service.Inspect().Socket, c.sb) && sandboxWorkAllowed(c.ctx, c.scope, c.id) && c.commandCurrent()
	}
	c.pending = true
	outcome, _ := c.service.registerWithInstallation(c.ctx, c.scope, &c.state, c.machines, c.sb, current, installationOptions{}, nil)
	if err := store.RecordSandboxStatus(c.scope, c.sb, outcome.Status, outcome.Detail, false); err != nil {
		return err
	}
	if err := saveResultError(store.StatusKey(c.scope, c.id), outcome); err != nil {
		return err
	}
	return c.verifyAddedEntry(outcome)

}
func (c *enabledChange) verifyAddedEntry(outcome result) error {
	// A completed add survives guest setup failures and cancellation. Read the
	// host catalog with a fresh read-only context to retain its actual entry.
	fresh, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), 10*time.Second)
	after, _, readErr := c.service.herdr.ListMachines(fresh)
	cancel()
	var verificationErr error
	if readErr != nil {
		verificationErr = fmt.Errorf("verify herdr entry after setup: %w", readErr)
	} else {
		c.machines = after
		actual, issue := associatedMachine(c.scope, c.sb, after, c.state)
		switch {
		case issue != "":
			verificationErr = errors.New(issue)
		case actual == nil:
			c.confirmed, c.pending = store.Disabled, false
			verificationErr = errors.New("the herdr entry is absent after setup")
		case actual.Enabled == nil:
			verificationErr = errors.New("the herdr connection state could not be verified after setup")
		default:
			c.confirmed, c.pending = store.Disabled, false
			if *actual.Enabled {
				c.confirmed = store.Enabled
			} else {
				verificationErr = errors.New("the herdr entry is disabled after setup")
			}
		}
	}
	if outcome.Status != "ready" {
		return errors.Join(errors.New(outcome.Detail), verificationErr)
	}
	return verificationErr
}
func (c *enabledChange) confirmEnableRuntime() error {
	p := c.service.Inspect()
	if p.Scope != c.scope {
		return errors.New("sandbox daemon changed before enabling its connection")
	}
	latest, err := c.service.inventory(c.ctx, p.Socket)
	if err != nil {
		return fmt.Errorf("cannot confirm sandbox runtime before enabling: %w", err)
	}
	found := false
	for _, candidate := range latest {
		if candidate.ID == c.id && candidate.Name == c.sb.Name {
			c.sb = candidate
			found = true
			break
		}
	}
	if !found {
		return errors.New("sandbox identity changed before enabling its connection")
	}
	if !strings.EqualFold(c.sb.Status, "running") && !strings.EqualFold(c.sb.Status, "stopped") {
		return errors.New("Sandbox runtime is not confirmed running or stopped. Refresh before enabling it.")
	}
	return nil
}
func (c *enabledChange) mutateNativeEnabled(want bool) error {
	if !c.commandCurrent() {
		return supersededEnabledChange()
	}
	if want && !sandboxWorkAllowed(c.ctx, c.scope, c.id) {
		return context.Canceled
	}
	if !c.commandCurrent() {
		return supersededEnabledChange()
	}
	c.pending = true
	output, mutationErr := c.service.herdr.SetMachineEnabled(c.ctx, c.machine.ID, want)
	// A cancelled or lost response may still have applied. Verify separately.
	fresh, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), 10*time.Second)
	after, _, readErr := c.service.herdr.ListMachines(fresh)
	cancel()
	if readErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("verify herdr connection: %w", readErr))
	}
	return c.verifyNativeEnabled(after, want, mutationErr, output)
}
func (c *enabledChange) saveEntry() error {
	item := store.Tracked{SandboxID: c.id, Target: c.machine.Target, ProfileID: c.machine.ID}
	for _, saved := range c.state.Scopes[c.scope] {
		if saved.SandboxID == c.id {
			item = saved
			break
		}
	}
	item.ProfileID, item.Target = c.machine.ID, c.machine.Target
	item.Label = c.machine.Label
	item.PendingLabel = ""
	item.LastObservedNativeEnabled = c.machine.Enabled
	if err := replaceTracked(&c.state, c.scope, item); err != nil {
		c.state.Scopes[c.scope] = append(c.state.Scopes[c.scope], item)
	}
	if err := store.SaveState(c.state); err != nil {
		return err
	}
	_, err := observeEntries(c.scope, []sbx.Sandbox{c.sb}, c.machines, c.state, false)
	return err
}

// RemoveEntry removes only a confirmed disabled entry and retains Disabled intent.
func (s *Service) RemoveEntry(ctx context.Context, scope, id string) error {
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	state, sb, _, machine, err := s.entryForAction(ctx, scope, id)
	if err != nil {
		return err
	}
	choices, err := store.ReadEnabled()
	if err != nil {
		return err
	}
	if choices[store.StatusKey(scope, id)] != store.Disabled {
		return errors.New("disable this sandbox before removing its herdr entry")
	}
	if machine == nil {
		return nil
	}
	if machine.Enabled == nil || *machine.Enabled {
		return errors.New("the herdr connection is not confirmed disabled; disable it before removal")
	}
	output, removeErr := s.herdr.RemoveMachine(ctx, machine.ID)
	after, _, readErr := s.herdr.ListMachines(ctx)
	if readErr != nil {
		return fmt.Errorf("removal outcome is unknown; saved label was kept: %w", readErr)
	}
	if machineByID(after, machine.ID) != nil {
		return unconfirmedEntryRemoval(scope, sb, after, state, removeErr, output)
	}
	clearAbsentProfile(&state, scope, id)
	if err := store.SaveState(state); err != nil {
		return err
	}
	_, err = observeEntries(scope, []sbx.Sandbox{sb}, after, state, false)
	return err
}

func (s *Service) entryForAction(ctx context.Context, scope, id string) (store.State, sbx.Sandbox, []herdr.Machine, *herdr.Machine, error) {
	state, err := store.LoadState()
	if err != nil {
		return state, sbx.Sandbox{}, nil, nil, err
	}
	p := s.Inspect()
	if p.Socket == "" || p.Scope != scope {
		return state, sbx.Sandbox{}, nil, nil, errors.New("sandbox daemon changed; refresh before this action")
	}
	inventory, err := s.inventory(ctx, p.Socket)
	if err != nil {
		return state, sbx.Sandbox{}, nil, nil, err
	}
	var selected sbx.Sandbox
	for _, sb := range inventory {
		if sb.ID == id {
			selected = sb
			break
		}
	}
	if selected.ID == "" {
		return state, selected, nil, nil, errors.New("sandbox identity is not present in current inventory")
	}
	machines, output, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return state, selected, nil, nil, fmt.Errorf("inspect herdr catalog: %s", commandError(err, output))
	}
	machine, issue := associatedMachine(scope, selected, machines, state)
	if issue != "" {
		return state, selected, machines, nil, errors.New(issue)
	}
	return state, selected, machines, machine, nil
}

func waitManagementLock(ctx context.Context) (func(), error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for the current reconciliation to finish: %w", err)
		}
		unlock, err := store.LockState()
		if err == nil || !errors.Is(err, store.ErrStateLocked) {
			return unlock, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for the current reconciliation to finish: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Rename changes a uniquely associated entry regardless of enabled intent.
func (s *Service) Rename(ctx context.Context, scope, id, label string) error {
	if strings.TrimSpace(label) == "" || strings.ContainsFunc(label, unicode.IsControl) {
		return errors.New("enter a nonempty herdr label without control characters")
	}
	unlock, err := waitManagementLock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	state, sb, machines, machine, err := s.entryForAction(ctx, scope, id)
	if err != nil {
		return err
	}
	if machine == nil {
		return errors.New("there is no herdr entry to rename")
	}
	if machine.Label == label {
		return nil
	}
	if machineLabelTaken(machines, machine.ID, label) {
		return errors.New("another herdr entry already uses this label")
	}
	return s.renameMachineEntry(ctx, scope, id, label, state, sb, machine)
}

// clearAbsentProfile abandons only an authoritatively absent catalog linkage.
// The sandbox choice and completed guest effects remain separate durable state.
func clearAbsentProfile(state *store.State, scope, id string) {
	for i := range state.Scopes[scope] {
		item := &state.Scopes[scope][i]
		if item.SandboxID == id && item.ProfileID != "" {
			item.ProfileID = ""
			item.Pending = false
			item.PendingLabel = ""
			item.LastObservedNativeEnabled = nil
		}
	}
}

// applyObservedStatus prevents an old Ready result from describing a currently
// disabled, stopped, absent, or unverified entry. Running setup failures remain.
func applyObservedStatus(cached *store.SandboxStatus, row SandboxRow) {
	if row.AssociationIssue != "" {
		cached.Connection = "association warning"
		cached.Detail = row.AssociationIssue
		return
	}
	if !row.EntryPresent && strings.EqualFold(row.Sandbox.Status, "stopped") {
		cached.Connection = "stopped"
		cached.Detail = "This sandbox is stopped and has no herdr entry."
		return
	}
	if row.Enabled == store.Disabled {
		applyDisabledObservation(cached, row)
		return
	}
	if nativeDisabledObservation(cached, row) {
		return
	}
	if strings.EqualFold(row.Sandbox.Status, "stopped") {
		applyStoppedObservation(cached, row)
		return
	}
	if !strings.EqualFold(row.Sandbox.Status, "running") {
		cached.Connection, cached.Detail = deferredRuntime(row.Sandbox.Status)
		return
	}
	applyRegisteredObservation(cached, row)
}

func deferredRuntime(runtime string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "pending", "starting", "creating", "stopping":
		return "pending", "Sandbox runtime is " + strings.ToLower(runtime) + ". Setup waits until it is confirmed running."
	case "failed", "error":
		return "runtime failed", "The sandbox reports a failed runtime state. Setup waits until it is confirmed running."
	default:
		return "runtime unknown", "Sandbox runtime is not confirmed. Setup waits for a current running observation."
	}
}

func cachedSandboxRows(scope string, sandboxes []sbx.Sandbox, status store.StatusStore, detail string) []SandboxRow {
	var rows []SandboxRow
	for _, sb := range sandboxes {
		cached := status.Sandboxes[store.StatusKey(scope, sb.ID)]
		choice := status.EnabledChoices[store.StatusKey(scope, sb.ID)]
		if choice == "" {
			choice = store.Enabled
		}
		cached.Stale = true
		cached.Detail = detail
		cached.Observed = sb.Status
		rows = append(rows, SandboxRow{Sandbox: sb, Scope: scope, Enabled: choice, EntryPresent: cached.EntryPresent, NativeEnabled: cached.NativeEnabled, Label: cached.Label, ProfileID: cached.ProfileID, AssociationIssue: "Current herdr catalog could not be verified.", Status: cached})
	}

	return rows
}

func captureSandboxBaseline(scope string, sb sbx.Sandbox, machines []herdr.Machine, initial store.StatusStore, state *store.State) error {
	choices := initial.EnabledChoices
	key := store.StatusKey(scope, sb.ID)
	choice := choices[key]
	if choice == "" {
		choice = store.Enabled
	}
	machine, issue := associatedMachine(scope, sb, machines, *state)
	if issue != "" {
		return nil
	}
	if machine == nil {
		clearAbsentProfile(state, scope, sb.ID)
		if initial.EnabledObservationPending[key] {
			choice = store.Disabled
		}
	} else if initial.EnabledObservationPending[key] && machine.Enabled == nil {
		return nil
	}
	if machine != nil {
		choice = rememberBaselineMachine(state, scope, sb.ID, machine, choice)
	}
	if _, err := store.SetEnabledIfRevision(scope, sb.ID, choice, initial.EnabledRevisions[key]); err != nil {
		return err
	}
	return nil
}

func applyDisabledObservation(cached *store.SandboxStatus, row SandboxRow) {
	if row.EntryPresent && (row.NativeEnabled == nil || *row.NativeEnabled) {
		cached.Connection = "disable not confirmed"
		cached.Detail = "Disabled is saved, but the native herdr connection has not been confirmed disabled. Retry Disable."
		return
	}
	cached.Connection = "disabled"
	cached.Detail = "This sandbox is disabled. Its existing herdr entry and label stay until you remove it."
	if !row.EntryPresent {
		cached.Detail = "This sandbox is disabled and has no herdr entry. Enable it to add an entry."
	}
	return
}

func applyStoppedObservation(cached *store.SandboxStatus, row SandboxRow) {
	if row.EntryPresent {
		if row.NativeEnabled == nil {
			cached.Connection = "enable not confirmed"
			cached.Detail = "Enabled is saved, but the native herdr connection state is unknown. Refresh or retry Enable."
			return
		}
		cached.Connection = "waiting for start"
		cached.Detail = "This sandbox is stopped. Its herdr entry is saved. Setup waits until it starts and Management is ON."
	}
	return
}

func savedAssociationChanged(saved *store.Tracked, m herdr.Machine, target string) bool {
	return saved != nil && saved.ProfileID != "" && (saved.ProfileID != m.ID || saved.Target != target)
}

func (c *enabledChange) prepare() error {
	var err error
	c.unlock, err = waitManagementLock(c.ctx)
	if err != nil {
		return err
	}
	c.state, c.sb, c.machines, c.machine, err = c.service.entryForAction(c.ctx, c.scope, c.id)
	if err != nil {
		return err
	}
	if !c.commandCurrent() {
		return supersededEnabledChange()
	}
	if err := c.observeBeforeChange(); err != nil {
		return err
	}

	return nil
}

func (c *enabledChange) verifyNativeEnabled(after []herdr.Machine, want bool, mutationErr error, output string) error {
	c.machines = after
	actual, issue := associatedMachine(c.scope, c.sb, after, c.state)
	if issue != "" {
		return errors.Join(mutationErr, errors.New(issue))
	}
	if actual == nil {
		c.confirmed, c.pending = store.Disabled, false
		return errors.Join(mutationErr, errors.New("the herdr entry is absent after the connection change"))
	}
	if actual.Enabled == nil {
		return errors.Join(mutationErr, errors.New("the herdr connection state could not be verified"))
	}
	c.machine = actual
	c.confirmed, c.pending = store.Disabled, false
	if *c.machine.Enabled {
		c.confirmed = store.Enabled
	}
	if *c.machine.Enabled != want {
		if mutationErr != nil {
			return fmt.Errorf("change herdr connection: %s", commandError(mutationErr, output))
		}
		return errors.New("herdr did not change the connection state")
	}
	return nil
}

func rememberRenamedMachine(state *store.State, scope, id string, observed *herdr.Machine) {
	item := store.Tracked{SandboxID: id, ProfileID: observed.ID, Target: observed.Target}
	for _, saved := range state.Scopes[scope] {
		if saved.SandboxID == id {
			item = saved
			break
		}
	}
	item.ProfileID, item.Target = observed.ID, observed.Target
	item.Label = observed.Label
	item.PendingLabel = ""
	item.CustomLabel = true
	if err := replaceTracked(state, scope, item); err != nil {
		state.Scopes[scope] = append(state.Scopes[scope], item)
	}

}

func machineLabelTaken(machines []herdr.Machine, id, label string) bool {
	for _, candidate := range machines {
		if candidate.ID != id && candidate.Label == label {
			return true
		}
	}
	return false
}

func unconfirmedEntryRemoval(scope string, sb sbx.Sandbox, after []herdr.Machine, state store.State, removeErr error, output string) error {
	_, _ = observeEntries(scope, []sbx.Sandbox{sb}, after, state, false)
	if removeErr != nil {
		return fmt.Errorf("remove herdr entry: %s", commandError(removeErr, output))
	}
	return errors.New("herdr entry removal is not confirmed; retry removal")
}

func rememberBaselineMachine(state *store.State, scope, id string, machine *herdr.Machine, choice store.EnabledChoice) store.EnabledChoice {
	item := store.Tracked{SandboxID: id, Target: machine.Target, ProfileID: machine.ID}
	for _, saved := range state.Scopes[scope] {
		if saved.SandboxID == id {
			item = saved
			break
		}
	}
	if machine.Enabled != nil {
		if *machine.Enabled {
			choice = store.Enabled
		} else {
			choice = store.Disabled
		}
	}
	item.ProfileID, item.Target = machine.ID, machine.Target
	item.Label = machine.Label
	item.PendingLabel = ""
	item.Pending = false
	item.LastObservedNativeEnabled = machine.Enabled
	if err := replaceTracked(state, scope, item); err != nil {
		state.Scopes[scope] = append(state.Scopes[scope], item)
	}
	return choice
}

func nativeDisabledObservation(cached *store.SandboxStatus, row SandboxRow) bool {
	if row.EntryPresent && (strings.EqualFold(row.Sandbox.Status, "running") || strings.EqualFold(row.Sandbox.Status, "stopped")) && row.NativeEnabled != nil && !*row.NativeEnabled {
		cached.Connection = "disabled in herdr"
		cached.Detail = "Enabled is saved, but the native herdr connection is disabled. Retry Enable to enable its connection."
		return true
	}

	return false
}

func applyRegisteredObservation(cached *store.SandboxStatus, row SandboxRow) {
	if row.EntryPresent && strings.EqualFold(row.Sandbox.Status, "running") && row.NativeEnabled != nil && *row.NativeEnabled {
		switch cached.Connection {
		case "disabled", "stopped", "waiting for start", "waiting for entry", "disabled in herdr", "disable not confirmed", "not registered", "ownership conflict", "association warning", "label already in use", "enable not confirmed":
			cached.Connection = "registered"
			cached.Detail = "This sandbox is registered in herdr. Setup has not been checked since its state changed."
		}
	}
	if !row.EntryPresent {
		switch cached.Connection {
		case "ready", "registered", "ownership conflict", "association warning", "waiting for entry", "label already in use":
			cached.Connection = "not registered"
			cached.Detail = "No herdr entry exists for this sandbox. Enable it to add an entry."
		}
	}
}

func (s *Service) renameMachineEntry(ctx context.Context, scope, id, label string, state store.State, sb sbx.Sandbox, machine *herdr.Machine) error {
	output, renameErr := s.herdr.RenameMachine(ctx, machine.ID, label)
	after, _, readErr := s.herdr.ListMachines(ctx)
	if readErr != nil {
		return fmt.Errorf("rename outcome is unknown; refresh before retrying: %w", readErr)
	}
	observed, issue := associatedMachine(scope, sb, after, state)
	if issue != "" || observed == nil {
		return errors.New("herdr entry identity changed during rename; inspect the catalog")
	}
	rememberRenamedMachine(&state, scope, id, observed)
	if err := store.SaveState(state); err != nil {
		return err
	}
	if _, err := observeEntries(scope, []sbx.Sandbox{sb}, after, state, false); err != nil {
		return err
	}
	if observed.Label == label {
		return nil
	}
	if renameErr != nil {
		return fmt.Errorf("rename herdr entry: %s", commandError(renameErr, output))
	}
	return errors.New("herdr label change is not confirmed; retry rename")
}
