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
}

func ownedMachineByID(machines []herdr.Machine, id, label, target string) *herdr.Machine {
	for i := range machines {
		candidate := &machines[i]
		if candidate.ID == id && candidate.Label == label && candidate.Target == target && usesDefaultSession(candidate.Session) {
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
	return s.reconcileMatching(ctx, out, match, nil)
}

func (s *Service) ReconcileContext(ctx context.Context, out io.Writer) error {
	return s.reconcileMatching(ctx, out, nil, nil)
}

func (s *Service) reconcileMatching(ctx context.Context, out io.Writer, match func(sbx.Sandbox) bool, current func(sbx.Sandbox) bool) error {
	return s.reconcileWithInstallation(ctx, out, match, current, installationOptions{})
}

func (s *Service) reconcileWithInstallation(ctx context.Context, out io.Writer, match func(sbx.Sandbox) bool, current func(sbx.Sandbox) bool, installation installationOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := s.Inspect()
	s.showChecks(out, p)
	if p.Scope == "" {
		return errors.New("cannot identify the local sandbox daemon scope")
	}
	unlock, err := store.LockState()
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.recoverPolicy(p.Scope, p.Socket); err != nil {
		return err
	}
	// A full refresh must recover deletions even when an empty inventory or
	// missing guest SSH setup prevents registering new machines.
	if match == nil && hasLevel(p.Checks, "Herdr", precheck.Ready) && hasLevel(p.Checks, "Sandbox capability", precheck.Ready) && hasLevel(p.Checks, "Sandbox daemon", precheck.Ready) && !hasBlocked(p.Checks, "Docker authorization") {
		if err := s.recoverDeletedSandboxes(ctx, out, p.Scope, p.Socket); err != nil {
			return err
		}
	}
	if hasBlocked(p.Checks, "Sandbox capability") || hasBlocked(p.Checks, "Sandbox daemon") || hasBlocked(p.Checks, "Docker authorization") || hasBlocked(p.Checks, "Sandbox SSH") {
		return errors.New("fix the blocked prerequisites before registering sandboxes")
	}
	if !hasLevel(p.Checks, "Sandbox capability", precheck.Ready) || !hasLevel(p.Checks, "Sandbox daemon", precheck.Ready) || !hasLevel(p.Checks, "Sandbox inventory", precheck.Ready) || !hasLevel(p.Checks, "Sandbox SSH", precheck.Ready) {
		return errors.New("complete the prerequisite checks before registering sandboxes")
	}

	installation.Socket = p.Socket
	if installation.Scope != p.Scope {
		installation.AuthorizedID = ""
	}
	installation.Scope = p.Scope

	sandboxes, err := s.inventory(ctx, p.Socket)
	if err != nil {
		return fmt.Errorf("list sandboxes: %w", err)
	}
	for _, sb := range sandboxes {
		if sb.ID != "" {
			if err := store.ObserveSandbox(p.Scope, sb); err != nil {
				return fmt.Errorf("save sandbox observation: %w", err)
			}
		}
	}
	machines, machineOutput, err := s.herdr.ListMachines(ctx)
	if err != nil {
		return fmt.Errorf("list Herdr machines: %s", commandError(err, machineOutput))
	}

	st, err := store.LoadState()
	if err != nil {
		return err
	}
	rows, err := observeManagement(p.Scope, sandboxes, machines, st)
	if err != nil {
		return err
	}
	management := map[string]store.ManagementChoice{}
	for _, row := range rows {
		management[row.Sandbox.ID] = row.Management
	}
	trackedByID := map[string]store.Tracked{}
	for _, item := range st.Scopes[p.Scope] {
		trackedByID[item.SandboxID] = item
	}

	results := []result{}
	resultSandboxes := map[string]sbx.Sandbox{}
	for _, sb := range sandboxes {
		if management[sb.ID] != store.Watched {
			if management[sb.ID] == store.Unmanaged && (match == nil || match(sb)) {
				results = append(results, result{Name: sb.Name, Status: "ownership conflict", Detail: "This sandbox is already a Herdr machine. It was added outside Herdr-sbx."})
				resultSandboxes[sb.Name] = sb
			}
			continue
		}
		if !strings.EqualFold(sb.Status, "running") {
			continue
		}
		if match != nil && !match(sb) {
			continue
		}
		if sb.ID == "" || sb.Name == "" {
			results = append(results, result{Name: sb.Name, Status: "failed", Detail: "sandbox has no stable identity"})
			resultSandboxes[sb.Name] = sb
			continue
		}
		registrationCurrent := func() bool { return s.sandboxRunning(p.Socket, sb) }
		if current != nil {
			registrationCurrent = func() bool { return current(sb) }
		}
		item, known := trackedByID[sb.ID]
		if known && item.ProfileID != "" {
			labelErr := s.recoverMachineLabel(ctx, p.Scope, &st, machines, sb, &item, registrationCurrent)
			if labelErr == nil {
				results = append(results, s.projectReadiness(ctx, p.Scope, &st, sb, &item, true, registrationCurrent))
				resultSandboxes[sb.Name] = sb
				continue
			}
			results = append(results, result{Name: sb.Name, Status: "ownership conflict", Detail: "saved machine label was not updated: " + labelErr.Error()})
			resultSandboxes[sb.Name] = sb
			continue
		}
		if known && item.Pending {
			// The catalog was read successfully under the registration lock.
			// Retry only when neither the recorded nor current identity collides.
			collision := false
			for _, machine := range machines {
				if machine.Label == item.Label || machine.Target == item.Target || machine.Label == sandboxMachineLabel(sb.Name) || machine.Target == sb.Name+".sbx" {
					collision = true
				}
			}
			if collision {
				results = append(results, result{Name: sb.Name, Status: "ownership conflict", Detail: "previous registration is unresolved; inspect the machine catalog before retrying"})
				resultSandboxes[sb.Name] = sb
				continue
			}
			// A previous attempt may have started the project server before
			// machine add rejected its version. Align before querying its panes.
			if err := s.recoverGuestVersion(ctx, p.Scope, sb, registrationCurrent, nil, false); err != nil {
				results = append(results, result{Name: sb.Name, Status: "setup required", Detail: "automatic Herdr alignment failed: " + err.Error()})
				resultSandboxes[sb.Name] = sb
				continue
			}
			item.Pending = false
			if err := replaceTracked(&st, p.Scope, item); err != nil {
				return err
			}
			if err := store.SaveState(st); err != nil {
				return err
			}
		}
		var progressStatusErr error
		outcome, saved := s.registerWithInstallation(ctx, p.Scope, &st, machines, sb, registrationCurrent, installation, func(message string) {
			if err := store.RecordSandboxStatus(p.Scope, sb, "preparing", message, false); err != nil {
				progressStatusErr = err
			}
			fmt.Fprintf(out, "%s: %s\n", sb.Name, message)
		})
		if progressStatusErr != nil {
			fmt.Fprintln(out, "Sandbox status was not saved:", progressStatusErr)
		}
		results = append(results, outcome)
		resultSandboxes[sb.Name] = sb
		if saved != nil {
			trackedByID[sb.ID] = *saved
		}
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "No running sandboxes to register.")
	}
	for _, item := range results {
		if sb, ok := resultSandboxes[item.Name]; ok && sb.ID != "" {
			if err := store.RecordSandboxStatus(p.Scope, sb, item.Status, item.Detail, false); err != nil {
				fmt.Fprintln(out, "Sandbox status was not saved:", err)
			}
		}
		if sb, ok := resultSandboxes[item.Name]; ok {
			if err := saveResultError(store.StatusKey(p.Scope, sb.ID), item); err != nil {
				fmt.Fprintln(out, "Sandbox diagnostic was not saved:", err)
			} else {
				s.notifyResult(ctx, out, p.Scope, sb.ID, item)
			}
		}
		fmt.Fprintf(out, "%-24s %-18s %s\n", item.Name, strings.ToUpper(item.Status), item.Detail)
	}
	return nil
}

func (s *Service) registerPrepared(scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox) (result, *store.Tracked) {
	return s.registerPreparedContext(context.Background(), scope, st, machines, sb, nil)
}

func (s *Service) registerPreparedContext(ctx context.Context, scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox, current func() bool) (result, *store.Tracked) {
	return s.registerPreparedWithProgress(ctx, scope, st, machines, sb, current, nil)
}

func (s *Service) registerPreparedWithProgress(ctx context.Context, scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox, current func() bool, progress func(string)) (result, *store.Tracked) {
	return s.registerWithInstallation(ctx, scope, st, machines, sb, current, installationOptions{}, progress)
}

func (s *Service) registerWithInstallation(ctx context.Context, scope string, st *store.State, machines []herdr.Machine, sb sbx.Sandbox, current func() bool, installation installationOptions, progress func(string)) (result, *store.Tracked) {
	target := sb.Name + ".sbx"
	if current != nil && !current() {
		return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed before registration"}, nil
	}
	for _, m := range machines {
		if m.Label == sandboxMachineLabel(sb.Name) || m.Target == target {
			if m.Target == target {
				return result{Name: sb.Name, Status: "ownership conflict", Detail: "This sandbox is already a Herdr machine. It was added outside Herdr-sbx."}, nil
			}
			return result{Name: sb.Name, Status: "ownership conflict", Detail: "Another Herdr machine already uses this sandbox's name. Review that machine in Herdr."}, nil
		}
	}

	remote, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return result{Name: sb.Name, Status: "setup required", Detail: "remote Herdr is unavailable: " + err.Error()}, nil
	}
	versionOutput := remote.Version
	if remote.Missing {
		preparationKey := scope + "/" + sb.ID
		allowed, cooldownErr := store.PreparationAllowed(preparationKey)
		if cooldownErr != nil {
			return result{Name: sb.Name, Status: "failed", Detail: "preparation status is unavailable: " + cooldownErr.Error()}, nil
		}
		if !allowed {
			detail, err := store.PreparationCooldownDetail(preparationKey)
			if err != nil {
				return result{Name: sb.Name, Status: "failed", Detail: "cannot read last preparation failure: " + err.Error()}, nil
			}
			return result{Name: sb.Name, Status: "setup required", Detail: detail}, nil
		}
		versionOutput, err = s.installHerdr(ctx, installationRequest{Scope: scope, Socket: installation.Socket, Sandbox: sb, Authorized: installation.Scope == scope && installation.AuthorizedID == sb.ID, Current: current, Progress: progress})
		if err != nil {
			if cooldownErr := store.NotePreparationFailure(preparationKey, err.Error(), s.preparationRetryDelay); cooldownErr != nil {
				return result{Name: sb.Name, Status: "failed", Detail: "preparation failed and its retry state was not saved: " + cooldownErr.Error()}, nil
			}
			if errors.Is(err, context.Canceled) {
				return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed while Herdr was being prepared"}, nil
			}
			var outcome *installationError
			if errors.As(err, &outcome) {
				return result{Name: sb.Name, Status: outcome.State, Detail: outcome.Detail}, nil
			}
			return result{Name: sb.Name, Status: "setup required", Detail: "remote Herdr preparation failed: " + err.Error()}, nil
		}
		if cooldownErr := store.ClearPreparationFailure(preparationKey); cooldownErr != nil {
			return result{Name: sb.Name, Status: "failed", Detail: "preparation completed but its retry state was not cleared: " + cooldownErr.Error()}, nil
		}
	} else if remote.Conflict {
		return result{Name: sb.Name, Status: "setup required", Detail: "remote Herdr path exists but is not executable; it was left unchanged"}, nil
	}
	if installedVersion := version.Parse(versionOutput); installedVersion == "" || version.Compare(installedVersion, minHerdrVersion) < 0 {
		return result{Name: sb.Name, Status: "setup required", Detail: "remote Herdr is missing or too old"}, nil
	}
	if current != nil && !current() {
		return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed before registration"}, nil
	}

	initialCWD, err := s.prepareInitialProjectWorkspace(ctx, scope, st, sb, current)
	if err != nil {
		return result{Name: sb.Name, Status: "workspace setup required", Detail: err.Error()}, nil
	}
	pending := store.Tracked{SandboxID: sb.ID, Label: sandboxMachineLabel(sb.Name), Target: target, Pending: true, ProjectWorkspacePending: initialCWD}
	if err := replaceTracked(st, scope, pending); err != nil {
		st.Scopes[scope] = append(st.Scopes[scope], pending)
	}
	if err := store.SaveState(*st); err != nil {
		return result{Name: sb.Name, Status: "failed", Detail: err.Error()}, nil
	}
	if current != nil && !current() {
		if err := removeTracked(st, scope, pending.SandboxID); err != nil || store.SaveState(*st) != nil {
			return result{Name: sb.Name, Status: "ownership conflict", Detail: "sandbox changed and registration intent could not be cleared"}, &pending
		}
		return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed before machine registration"}, nil
	}

	profileID, addOutput, addErr := s.herdr.AddMachine(ctx, pending.Label, target)
	if addErr != nil {
		if strings.Contains(addOutput, "remote server is not ready for saved machines") {
			if err := s.recoverGuestVersion(ctx, scope, sb, current, progress, true); err != nil {
				return result{Name: sb.Name, Status: "setup required", Detail: "automatic Herdr alignment failed: " + err.Error()}, &pending
			}
			return result{Name: sb.Name, Status: "retrying", Detail: "Herdr aligned; registration will retry on the next scan"}, &pending
		}
		if deterministicAddFailure(addOutput) {
			if err := removeTracked(st, scope, pending.SandboxID); err != nil {
				return result{Name: sb.Name, Status: "ownership conflict", Detail: "machine add failed and its intent could not be cleared"}, &pending
			}
			if err := store.SaveState(*st); err != nil {
				return result{Name: sb.Name, Status: "ownership conflict", Detail: "machine add failed and its cleared intent could not be saved"}, &pending
			}
			return addFailureResult(sb.Name, addErr, addOutput), nil
		}
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "registration outcome is uncertain: " + commandError(addErr, addOutput)}, &pending
	}

	if profileID == "" {
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "registration succeeded but did not return a trusted machine ID"}, &pending
	}
	after, _, listErr := s.herdr.ListMachines(ctx)
	if listErr != nil {
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "registration completed but could not inspect the returned machine ID"}, &pending
	}
	if ownedMachineByID(after, profileID, pending.Label, target) == nil {
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "registration did not yield the returned owned machine"}, &pending
	}

	pending.ProfileID = profileID
	pending.Pending = false
	if err := replaceTracked(st, scope, pending); err != nil {
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "machine was registered but ownership could not be saved"}, &pending
	}
	if err := store.SaveState(*st); err != nil {
		return result{Name: sb.Name, Status: "ownership conflict", Detail: "machine was registered but ownership could not be saved"}, &pending
	}
	if current != nil && !current() {
		return result{Name: sb.Name, Status: "stale", Detail: "sandbox changed after machine registration; cleanup is pending"}, &pending
	}
	return s.projectReadiness(ctx, scope, st, sb, &pending, false, current), &pending
}

func (s *Service) sandboxRunning(socket string, expected sbx.Sandbox) bool {
	sandboxes, err := s.inventory(context.Background(), socket)
	if err != nil {
		return false
	}
	for _, sb := range sandboxes {
		if sb.ID == expected.ID && sb.Name == expected.Name && strings.EqualFold(sb.Status, "running") {
			return true
		}
	}
	return false
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
	if item.Status == "plugin setup required" || item.Status == "manual intervention required" {
		if err := store.SetRegistrationError("", scope, ""); err != nil {
			return err
		}
		if len(item.PluginFailures) == 0 {
			// Catalog/defaults could not be read. No plugin recovery was observed.
			return store.RecordGuestPluginFailureCode(store.CodeGuestPluginSync, scope, item.Detail)
		}
		return store.SetGuestPluginFailures(scope, item.PluginFailures)
	}
	code := store.CodeRegistration
	switch item.Status {
	case "ownership conflict":
		code = store.CodeOwnershipConflict
	case "workspace setup required":
		code = store.CodeWorkspaceSetup
	case "setup required":
		code = store.CodeSandboxSetup
	}
	if item.DiagnosticCode != "" {
		code = item.DiagnosticCode
	}
	return store.SetRegistrationError(code, scope, item.Detail)
}
