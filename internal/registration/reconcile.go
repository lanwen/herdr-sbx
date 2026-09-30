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
	Name   string
	Status string
	Detail string
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
	trackedByID := map[string]store.Tracked{}
	for _, item := range st.Scopes[p.Scope] {
		trackedByID[item.SandboxID] = item
	}

	results := []result{}
	resultSandboxes := map[string]sbx.Sandbox{}
	for _, sb := range sandboxes {
		if !strings.EqualFold(sb.Status, "running") {
			continue
		}
		if match != nil && !match(sb) {
			continue
		}
		if sb.ID == "" || sb.Name == "" {
			results = append(results, result{sb.Name, "failed", "sandbox has no stable identity"})
			resultSandboxes[sb.Name] = sb
			continue
		}
		registrationCurrent := func() bool { return s.sandboxRunning(p.Socket, sb) }
		if current != nil {
			registrationCurrent = func() bool { return current(sb) }
		}
		item, known := trackedByID[sb.ID]
		if known && item.ProfileID != "" {
			labelErr := s.migrateMachineLabel(ctx, p.Scope, &st, machines, sb, &item, registrationCurrent)
			if labelErr == nil {
				results = append(results, s.projectReadiness(ctx, p.Scope, &st, sb, &item, true, registrationCurrent))
				resultSandboxes[sb.Name] = sb
				continue
			}
			results = append(results, result{sb.Name, "ownership conflict", "saved machine label was not updated: " + labelErr.Error()})
			resultSandboxes[sb.Name] = sb
			continue
		}
		if known && item.Pending {
			results = append(results, result{sb.Name, "ownership conflict", "previous registration is unresolved; inspect the machine catalog before retrying"})
			resultSandboxes[sb.Name] = sb
			continue
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
		results = append(results, result{sb.Name, outcome.Status, outcome.Detail})
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
		return result{sb.Name, "stale", "sandbox changed before registration"}, nil
	}
	for _, m := range machines {
		if m.Label == sandboxMachineLabel(sb.Name) || m.Target == target {
			return result{sb.Name, "ownership conflict", "a manual or untracked machine uses this label or target"}, nil
		}
	}

	remote, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return result{sb.Name, "setup required", "remote Herdr is unavailable: " + err.Error()}, nil
	}
	versionOutput := remote.Version
	if remote.Missing {
		preparationKey := scope + "/" + sb.ID
		allowed, cooldownErr := store.PreparationAllowed(preparationKey)
		if cooldownErr != nil {
			return result{sb.Name, "failed", "preparation status is unavailable: " + cooldownErr.Error()}, nil
		}
		if !allowed {
			detail, err := store.PreparationCooldownDetail(preparationKey)
			if err != nil {
				return result{sb.Name, "failed", "cannot read last preparation failure: " + err.Error()}, nil
			}
			return result{sb.Name, "setup required", detail}, nil
		}
		versionOutput, err = s.installHerdr(ctx, installationRequest{Scope: scope, Socket: installation.Socket, Sandbox: sb, Authorized: installation.Scope == scope && installation.AuthorizedID == sb.ID, Current: current, Progress: progress})
		if err != nil {
			if cooldownErr := store.NotePreparationFailure(preparationKey, err.Error(), s.preparationRetryDelay); cooldownErr != nil {
				return result{sb.Name, "failed", "preparation failed and its retry state was not saved: " + cooldownErr.Error()}, nil
			}
			if errors.Is(err, context.Canceled) {
				return result{sb.Name, "stale", "sandbox changed while Herdr was being prepared"}, nil
			}
			var outcome *installationError
			if errors.As(err, &outcome) {
				return result{sb.Name, outcome.State, outcome.Detail}, nil
			}
			return result{sb.Name, "setup required", "remote Herdr preparation failed: " + err.Error()}, nil
		}
		if cooldownErr := store.ClearPreparationFailure(preparationKey); cooldownErr != nil {
			return result{sb.Name, "failed", "preparation completed but its retry state was not cleared: " + cooldownErr.Error()}, nil
		}
	} else if remote.Conflict {
		return result{sb.Name, "setup required", "remote Herdr path exists but is not executable; it was left unchanged"}, nil
	}
	if installedVersion := version.Parse(versionOutput); installedVersion == "" || version.Compare(installedVersion, minHerdrVersion) < 0 {
		return result{sb.Name, "setup required", "remote Herdr is missing or too old"}, nil
	}
	if current != nil && !current() {
		return result{sb.Name, "stale", "sandbox changed before registration"}, nil
	}

	pending := store.Tracked{SandboxID: sb.ID, Label: sandboxMachineLabel(sb.Name), Target: target, Pending: true}
	st.Scopes[scope] = append(st.Scopes[scope], pending)
	if err := store.SaveState(*st); err != nil {
		return result{sb.Name, "failed", err.Error()}, nil
	}
	if current != nil && !current() {
		if err := removeTracked(st, scope, pending.SandboxID); err != nil || store.SaveState(*st) != nil {
			return result{sb.Name, "ownership conflict", "sandbox changed and registration intent could not be cleared"}, &pending
		}
		return result{sb.Name, "stale", "sandbox changed before machine registration"}, nil
	}

	profileID, addOutput, addErr := s.herdr.AddMachine(ctx, pending.Label, target)
	if addErr != nil {
		if deterministicAddFailure(addOutput) {
			if err := removeTracked(st, scope, pending.SandboxID); err != nil {
				return result{sb.Name, "ownership conflict", "machine add failed and its intent could not be cleared"}, &pending
			}
			if err := store.SaveState(*st); err != nil {
				return result{sb.Name, "ownership conflict", "machine add failed and its cleared intent could not be saved"}, &pending
			}
			return addFailureResult(sb.Name, addErr, addOutput), nil
		}
		return result{sb.Name, "ownership conflict", "registration outcome is uncertain: " + commandError(addErr, addOutput)}, &pending
	}

	if profileID == "" {
		return result{sb.Name, "ownership conflict", "registration succeeded but did not return a trusted machine ID"}, &pending
	}
	after, _, listErr := s.herdr.ListMachines(ctx)
	if listErr != nil {
		return result{sb.Name, "ownership conflict", "registration completed but could not inspect the returned machine ID"}, &pending
	}
	if ownedMachineByID(after, profileID, pending.Label, target) == nil {
		return result{sb.Name, "ownership conflict", "registration did not yield the returned owned machine"}, &pending
	}

	pending.ProfileID = profileID
	pending.Pending = false
	if err := replaceTracked(st, scope, pending); err != nil {
		return result{sb.Name, "ownership conflict", "machine was registered but ownership could not be saved"}, &pending
	}
	if err := store.SaveState(*st); err != nil {
		return result{sb.Name, "ownership conflict", "machine was registered but ownership could not be saved"}, &pending
	}
	if current != nil && !current() {
		return result{sb.Name, "stale", "sandbox changed after machine registration; cleanup is pending"}, &pending
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

func (s *Service) readinessResult(ctx context.Context, name, profileID string, alreadyKnown bool, current func() bool) result {
	statusOutput, err := s.herdr.MachineStatus(ctx, profileID)
	if err != nil {
		return result{name, "failed", "saved machine is not ready: " + commandError(err, statusOutput) + ". Retry registration when the remote server is available."}
	}
	if current != nil && !current() {
		return result{name, "stale", "sandbox changed after remote readiness; cleanup is pending"}
	}
	if alreadyKnown {
		return result{name, "ready", "already registered and reachable"}
	}
	return result{name, "ready", "registered and reachable"}
}

func addFailureResult(name string, err error, output string) result {
	detail := commandError(err, output)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "capacity") || strings.Contains(lower, "catalog full") || strings.Contains(lower, "maximum") || strings.Contains(lower, "limit") {
		return result{name, "catalog full", "Herdr cannot save another machine: " + detail + ". Remove an unused machine, then retry."}
	}
	return result{name, "failed", "machine add failed: " + detail + ". Retry after correcting the reported problem."}
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
