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
	var saved *store.Tracked
	for otherScope, entries := range state.Scopes {
		for i := range entries {
			entry := &entries[i]
			if otherScope == scope && entry.SandboxID == sb.ID {
				if saved != nil {
					return nil, "More than one saved association identifies this sandbox."
				}
				saved = entry
				continue
			}
			if entry.Target == target && (otherScope != scope || entry.SandboxID != sb.ID) {
				return nil, "This SSH target belongs to another saved sandbox identity."
			}
		}
	}
	var candidates []herdr.Machine
	for _, machine := range machines {
		if machine.Target == target {
			candidates = append(candidates, machine)
		}
	}
	if len(candidates) > 1 {
		return nil, "More than one herdr entry uses this sandbox target."
	}
	if len(candidates) == 0 {
		if saved != nil && saved.ProfileID != "" && machineByID(machines, saved.ProfileID) != nil {
			return nil, "The saved herdr entry now uses another target; inspect its association."
		}
		return nil, ""
	}
	m := candidates[0]
	if m.ID == "" || !usesDefaultSession(m.Session) {
		return nil, "The herdr entry has an unknown identity or another session."
	}
	if saved != nil && saved.ProfileID != "" && (saved.ProfileID != m.ID || saved.Target != target) {
		return nil, "The saved herdr entry identity changed. Refresh and inspect the association."
	}
	for otherScope, entries := range state.Scopes {
		for _, entry := range entries {
			if entry.ProfileID == m.ID && (otherScope != scope || entry.SandboxID != sb.ID) {
				return nil, "This herdr entry is associated with another sandbox."
			}
		}
	}
	return &m, ""
}

func observeManagement(scope string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State) ([]SandboxRow, error) {
	return observeEntries(scope, sandboxes, machines, state, true)
}
func observeEntries(scope string, sandboxes []sbx.Sandbox, machines []herdr.Machine, state store.State, complete bool, observedRevisions ...map[string]uint64) ([]SandboxRow, error) {
	var rows []SandboxRow
	err := store.UpdateStatusStore(func(status *store.StatusStore) {
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
		for _, sb := range sandboxes {
			if sb.ID == "" {
				continue
			}
			key := store.StatusKey(scope, sb.ID)
			choice := status.EnabledChoices[key]
			if choice == "" {
				choice = store.Enabled
				status.EnabledChoices[key] = choice
			}
			if len(observedRevisions) > 0 && observedRevisions[0][key] != status.EnabledRevisions[key] {
				// This catalog predates a newer command. Preserve its confirmed
				// facts and do not reconcile from the superseded observation.
				cached := status.Sandboxes[key]
				cached.Enabled, cached.Stale = choice, true
				rows = append(rows, SandboxRow{Scope: scope, Sandbox: sb, Enabled: choice, EntryPresent: cached.EntryPresent, NativeEnabled: cached.NativeEnabled, Label: cached.Label, ProfileID: cached.ProfileID, AssociationIssue: cached.AssociationIssue, ObservationPending: true, Status: cached})
				continue
			}
			row := SandboxRow{Scope: scope, Sandbox: sb, Enabled: choice}
			machine, issue := associatedMachine(scope, sb, machines, state)
			row.AssociationIssue = issue
			freshForPending := false
			if len(observedRevisions) > 0 {
				before, known := observedRevisions[0][key]
				freshForPending = known && before == status.EnabledRevisions[key]
			}
			if status.EnabledObservationPending[key] && freshForPending && issue == "" && (machine == nil || machine.Enabled != nil) {
				choice = store.Disabled
				if machine != nil && *machine.Enabled {
					choice = store.Enabled
				}
				status.EnabledChoices[key] = choice
				status.EnabledRevisions[key]++
				delete(status.EnabledObservationPending, key)
				row.Enabled = choice
			}
			row.ObservationPending = status.EnabledObservationPending[key]
			// The exposed CLI has no offline disabled-entry operation. Keep an absent,
			// stopped sandbox disabled instead of scheduling a wake-capable add.
			if issue == "" && machine == nil && strings.EqualFold(sb.Status, "stopped") {
				choice = store.Disabled
				status.EnabledChoices[key] = choice
				row.Enabled = choice
			}
			// A generated-label collision blocks creating an entry, even though
			// labels do not establish sandbox identity.
			labelCollision := false
			if issue == "" && machine == nil && choice == store.Enabled && strings.EqualFold(sb.Status, "running") {
				for _, candidate := range machines {
					if candidate.Label == sandboxMachineLabel(sb.Name) {
						labelCollision = true
						break
					}
				}
			}
			if issue == "" {
				for diagnosticKey, diagnostic := range status.Errors {
					if diagnostic.Scope == key && (diagnostic.Code == store.CodeOwnershipConflict && !labelCollision || diagnostic.Code == store.CodeSafeEntryUnsupported) {
						delete(status.Errors, diagnosticKey)
					}
				}
			}
			if machine != nil {
				row.EntryPresent = true

				row.Label = machine.Label
				row.ProfileID = machine.ID
				row.NativeEnabled = machine.Enabled
			}
			cached := status.Sandboxes[key]
			cached.Scope, cached.SandboxID, cached.Name, cached.Observed = scope, sb.ID, sb.Name, sb.Status
			cached.Enabled, cached.EntryPresent, cached.NativeEnabled, cached.AssociationIssue = choice, row.EntryPresent, row.NativeEnabled, issue
			cached.Label, cached.ProfileID = row.Label, row.ProfileID
			cached.ObservedAt = time.Now()
			cached.ConfirmPresent()
			applyObservedStatus(&cached, row)
			if labelCollision {
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
			row.Status = cached
			rows = append(rows, row)
		}
	})
	return rows, err
}

// CaptureBaseline accepts verified external changes before a new management period.
// It never treats a missing entry as permission to reset a Disabled choice.
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
	choices := initial.EnabledChoices
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
		key := store.StatusKey(scope, sb.ID)
		choice := choices[key]
		if choice == "" {
			choice = store.Enabled
		}
		machine, issue := associatedMachine(scope, sb, machines, state)
		if issue != "" {
			continue
		}
		if machine == nil {
			clearAbsentProfile(&state, scope, sb.ID)
			if initial.EnabledObservationPending[key] {
				choice = store.Disabled
			}
		} else if initial.EnabledObservationPending[key] && machine.Enabled == nil {
			continue
		}
		if machine != nil {
			item := store.Tracked{SandboxID: sb.ID, Target: machine.Target, ProfileID: machine.ID}
			for _, saved := range state.Scopes[scope] {
				if saved.SandboxID == sb.ID {
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
			if err := replaceTracked(&state, scope, item); err != nil {
				state.Scopes[scope] = append(state.Scopes[scope], item)
			}
		}
		if _, err := store.SetEnabledIfRevision(scope, sb.ID, choice, initial.EnabledRevisions[key]); err != nil {
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
func (s *Service) SetEnabled(ctx context.Context, scope, id string, choice store.EnabledChoice) (returnErr error) {
	ctx = WithExplicitAction(ctx)
	if choice != store.Enabled && choice != store.Disabled {
		return errors.New("invalid enabled choice")
	}
	var state store.State
	var sb sbx.Sandbox
	var machines []herdr.Machine
	var machine *herdr.Machine
	observed := false
	var unlock func()
	// Accept before waiting; retain confirmed state in the same transaction.
	revision, confirmed, pending, err := store.BeginEnabledChange(scope, id, choice)
	if err != nil {
		return err
	}
	defer func() {
		if unlock != nil {
			defer unlock()
		}
		applied, settleErr := store.SettleEnabledChoice(scope, id, confirmed, revision, pending)
		returnErr = errors.Join(returnErr, settleErr)
		if applied && observed {
			_, observeErr := observeEntries(scope, []sbx.Sandbox{sb}, machines, state, false)
			returnErr = errors.Join(returnErr, observeErr)
		}
	}()
	commandCurrent := func() bool {
		saved, err := store.ReadStatusStore()
		return err == nil && saved.EnabledRevisions[store.StatusKey(scope, id)] == revision
	}
	superseded := func() error { return fmt.Errorf("a newer sandbox choice replaced this action: %w", context.Canceled) }
	unlock, err = waitManagementLock(ctx)
	if err != nil {
		return err
	}
	state, sb, machines, machine, err = s.entryForAction(ctx, scope, id)
	if err != nil {
		return err
	}
	if !commandCurrent() {
		return superseded()
	}
	observed = true
	if machine == nil {
		confirmed, pending = store.Disabled, false
	} else if machine.Enabled != nil {
		confirmed, pending = store.Disabled, false
		if *machine.Enabled {
			confirmed = store.Enabled
		}
	}
	// Keep a last-confirmed entry observation if the mutation outcome is lost.
	if _, err := observeEntries(scope, []sbx.Sandbox{sb}, machines, state, false); err != nil {
		return err
	}
	if machine == nil {
		if choice == store.Disabled {
			return nil
		}
		if !strings.EqualFold(sb.Status, "running") {
			if !strings.EqualFold(sb.Status, "stopped") {
				status, detail := deferredRuntime(sb.Status)
				_ = store.RecordSandboxStatus(scope, sb, status, detail, false)
				return errors.New("Sandbox runtime is not confirmed running. Refresh before enabling it.")
			}
			return nil
		}
		// Ordinary add and guest setup are permitted only after rechecking RUNNING.
		current := func() bool {
			return s.sandboxRunning(s.Inspect().Socket, sb) && sandboxWorkAllowed(ctx, scope, id) && commandCurrent()
		}
		pending = true
		outcome, _ := s.registerWithInstallation(ctx, scope, &state, machines, sb, current, installationOptions{}, nil)
		if err := store.RecordSandboxStatus(scope, sb, outcome.Status, outcome.Detail, false); err != nil {
			return err
		}
		if err := saveResultError(store.StatusKey(scope, id), outcome); err != nil {
			return err
		}
		// A completed add survives guest setup failures and cancellation. Read the
		// host catalog with a fresh read-only context to retain its actual entry.
		fresh, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		after, _, readErr := s.herdr.ListMachines(fresh)
		cancel()
		var verificationErr error
		if readErr != nil {
			verificationErr = fmt.Errorf("verify herdr entry after setup: %w", readErr)
		} else {
			machines = after
			actual, issue := associatedMachine(scope, sb, after, state)
			switch {
			case issue != "":
				verificationErr = errors.New(issue)
			case actual == nil:
				confirmed, pending = store.Disabled, false
				verificationErr = errors.New("the herdr entry is absent after setup")
			case actual.Enabled == nil:
				verificationErr = errors.New("the herdr connection state could not be verified after setup")
			default:
				confirmed, pending = store.Disabled, false
				if *actual.Enabled {
					confirmed = store.Enabled
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
	if choice == store.Enabled {
		p := s.Inspect()
		if p.Scope != scope {
			return errors.New("sandbox daemon changed before enabling its connection")
		}
		latest, err := s.inventory(ctx, p.Socket)
		if err != nil {
			return fmt.Errorf("cannot confirm sandbox runtime before enabling: %w", err)
		}
		found := false
		for _, observed := range latest {
			if observed.ID == id && observed.Name == sb.Name {
				sb = observed
				found = true
				break
			}
		}
		if !found {
			return errors.New("sandbox identity changed before enabling its connection")
		}
		if !strings.EqualFold(sb.Status, "running") && !strings.EqualFold(sb.Status, "stopped") {
			return errors.New("Sandbox runtime is not confirmed running or stopped. Refresh before enabling it.")
		}
	}
	want := choice == store.Enabled
	if machine.Enabled == nil || *machine.Enabled != want {
		if !commandCurrent() {
			return superseded()
		}
		if want && !sandboxWorkAllowed(ctx, scope, id) {
			return context.Canceled
		}
		if !commandCurrent() {
			return superseded()
		}
		pending = true
		output, mutationErr := s.herdr.SetMachineEnabled(ctx, machine.ID, want)
		// A cancelled or lost response may still have applied. Verify separately.
		fresh, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		after, _, readErr := s.herdr.ListMachines(fresh)
		cancel()
		if readErr != nil {
			return errors.Join(mutationErr, fmt.Errorf("verify herdr connection: %w", readErr))
		}
		machines = after
		actual, issue := associatedMachine(scope, sb, after, state)
		if issue != "" {
			return errors.Join(mutationErr, errors.New(issue))
		}
		if actual == nil {
			confirmed, pending = store.Disabled, false
			return errors.Join(mutationErr, errors.New("the herdr entry is absent after the connection change"))
		}
		if actual.Enabled == nil {
			return errors.Join(mutationErr, errors.New("the herdr connection state could not be verified"))
		}
		machine = actual
		confirmed, pending = store.Disabled, false
		if *machine.Enabled {
			confirmed = store.Enabled
		}
		if *machine.Enabled != want {
			if mutationErr != nil {
				return fmt.Errorf("change herdr connection: %s", commandError(mutationErr, output))
			}
			return errors.New("herdr did not change the connection state")
		}
	}
	if !commandCurrent() {
		_, _ = observeEntries(scope, []sbx.Sandbox{sb}, machines, state, false)
		return superseded()
	}
	item := store.Tracked{SandboxID: id, Target: machine.Target, ProfileID: machine.ID}
	for _, saved := range state.Scopes[scope] {
		if saved.SandboxID == id {
			item = saved
			break
		}
	}
	item.ProfileID, item.Target = machine.ID, machine.Target
	item.Label = machine.Label
	item.PendingLabel = ""
	item.LastObservedNativeEnabled = machine.Enabled
	if err := replaceTracked(&state, scope, item); err != nil {
		state.Scopes[scope] = append(state.Scopes[scope], item)
	}
	if err := store.SaveState(state); err != nil {
		return err
	}
	_, err = observeEntries(scope, []sbx.Sandbox{sb}, machines, state, false)
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
		_, _ = observeEntries(scope, []sbx.Sandbox{sb}, after, state, false)
		if removeErr != nil {
			return fmt.Errorf("remove herdr entry: %s", commandError(removeErr, output))
		}
		return errors.New("herdr entry removal is not confirmed; retry removal")
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
	for _, candidate := range machines {
		if candidate.ID != machine.ID && candidate.Label == label {
			return errors.New("another herdr entry already uses this label")
		}
	}
	output, renameErr := s.herdr.RenameMachine(ctx, machine.ID, label)
	after, _, readErr := s.herdr.ListMachines(ctx)
	if readErr != nil {
		return fmt.Errorf("rename outcome is unknown; refresh before retrying: %w", readErr)
	}
	observed, issue := associatedMachine(scope, sb, after, state)
	if issue != "" || observed == nil {
		return errors.New("herdr entry identity changed during rename; inspect the catalog")
	}
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
	if err := replaceTracked(&state, scope, item); err != nil {
		state.Scopes[scope] = append(state.Scopes[scope], item)
	}
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
	if row.EntryPresent && (strings.EqualFold(row.Sandbox.Status, "running") || strings.EqualFold(row.Sandbox.Status, "stopped")) && row.NativeEnabled != nil && !*row.NativeEnabled {
		cached.Connection = "disabled in herdr"
		cached.Detail = "Enabled is saved, but the native herdr connection is disabled. Retry Enable to enable its connection."
		return
	}
	if strings.EqualFold(row.Sandbox.Status, "stopped") {
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
	if !strings.EqualFold(row.Sandbox.Status, "running") {
		cached.Connection, cached.Detail = deferredRuntime(row.Sandbox.Status)
		return
	}
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
