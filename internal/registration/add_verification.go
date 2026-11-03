package registration

import (
	"context"
	"errors"
	"fmt"
	"github.com/lanwen/herdr-sbx/internal/process"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

const addVerificationAttempts = 3
const addVerificationDelay = 150 * time.Millisecond
const addVerificationTimeout = 5 * time.Second

type addCatalogObservation struct {
	machines []herdr.Machine
	detail   string
	err      error
	checks   int
}

// Verification never repeats add or opens a guest connection.
func (a *registrationAttempt) verifyAddedEntry() *result {
	ctx, cancel := context.WithTimeout(a.ctx, addVerificationTimeout)
	defer cancel()
	observed := addCatalogObservation{}
	for observed.checks < addVerificationAttempts && ctx.Err() == nil {
		var output string
		started := time.Now()
		observed.machines, output, observed.err = a.service.herdr.ListMachines(ctx)
		observed.checks++
		evidence := output
		if observed.err == nil {
			evidence = fmt.Sprintf("%d catalog entries read; acknowledged ID %s", len(observed.machines), a.pending.PendingProfileID)
		}
		a.service.logRegistration(ctx, registrationLog{Stage: "entry_verification", Scope: a.scope, Sandbox: a.sandbox, Attempt: observed.checks, Started: started, Timeout: 5 * time.Second, Ack: a.pending.PendingProfileID, Err: observed.err, Output: evidence})
		if observed.err == nil && a.confirmAddedEntry(observed.machines) {
			return nil
		}
		if observed.err != nil {
			observed.detail = commandError(observed.err, output)
		}
		if observed.checks < addVerificationAttempts && !waitAddVerification(ctx) {
			break
		}
	}
	return a.unconfirmedAdd(observed, ctx.Err())
}

func waitAddVerification(ctx context.Context) bool {
	timer := time.NewTimer(addVerificationDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *registrationAttempt) confirmAddedEntry(machines []herdr.Machine) bool {
	if a.pending.PendingProfileID == "" {
		return false
	}
	machine, issue := associatedMachine(a.scope, a.sandbox, machines, *a.state)
	if issue != "" || machine == nil || machine.ID != a.pending.PendingProfileID {
		return false
	}
	a.pending.Label = machine.Label
	a.pending.LastObservedNativeEnabled = machine.Enabled
	return true
}

func (a *registrationAttempt) unconfirmedAdd(observed addCatalogObservation, stopped error) *result {
	detail := fmt.Sprintf("No saved Herdr connection appeared after %d checks.", observed.checks)
	code := store.CodeRegistration
	if observed.err != nil || stopped != nil {
		cause := observed.detail
		if stopped != nil {
			cause = stopped.Error()
		}
		detail = fmt.Sprintf("The plugin could not check whether Herdr saved the connection: %s. It will check again automatically.", cause)
	} else if issue := a.addCatalogConflict(observed.machines); issue != "" {
		detail, code = issue, store.CodeOwnershipConflict
	} else {
		a.catalogAbsent = true
	}
	return &result{Name: a.sandbox.Name, Status: "registration unconfirmed", DiagnosticCode: code, Detail: detail}
}

func (a *registrationAttempt) addCatalogConflict(machines []herdr.Machine) string {
	_, issue := associatedMachine(a.scope, a.sandbox, machines, *a.state)
	if issue != "" {
		return issue
	}
	if pendingCollision(machines, a.pending, a.sandbox) {
		return "Another saved Herdr connection uses this sandbox's name or address. Registration is paused to avoid changing it."
	}
	return ""
}

func pendingRegistrationDetail(status store.StatusStore, key string) string {
	latest := store.ErrorRecord{}
	for _, entry := range status.Errors {
		if entry.Scope == key && (entry.Code == store.CodeRegistration || entry.Code == store.CodeOwnershipConflict) && (latest.Detail == "" || entry.LastSeen.After(latest.LastSeen)) {
			latest = entry
		}
	}
	if latest.Detail != "" {
		return latest.Detail
	}
	return "The earlier registration attempt did not record its result."
}

// The known server-not-ready recovery gets one retry, after fresh absence.
func (a *registrationAttempt) retryAfterRepair() (result, *store.Tracked) {
	a.repairRetried = true
	if err := a.service.recoverGuestVersion(a.ctx, sandboxWork{Scope: a.scope, Sandbox: a.sandbox, Current: a.current}, a.progress, true); err != nil {
		return result{Name: a.sandbox.Name, Status: "setup required", Detail: "Automatic Herdr alignment failed: " + err.Error()}, &a.pending
	}
	return a.retryAfterReadiness()
}

func transientAddFailure(output string, err error) bool {
	lower := strings.ToLower(output)
	return errors.Is(err, process.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "platform detection failed") || (strings.Contains(lower, "sandbox") && strings.Contains(lower, "unavailable"))
}

func (a *registrationAttempt) retryTransient() (result, *store.Tracked) {
	a.repairRetried = true
	if a.current == nil || !a.current() {
		return a.currentResult("Registration paused because the sandbox could not be verified"), &a.pending
	}
	if outcome := a.prepareHerdr(); outcome != nil {
		return *outcome, &a.pending
	}
	return a.retryAfterReadiness()
}

func (a *registrationAttempt) retryAfterReadiness() (result, *store.Tracked) {
	if a.current == nil || !a.current() {
		return a.currentResult("Registration paused because the sandbox could not be verified"), &a.pending
	}
	after, output, err := a.service.herdr.ListMachines(a.ctx)
	if err != nil {
		return result{Name: a.sandbox.Name, Status: "registration unconfirmed", Detail: "The entry could not be verified before automatic retry: " + commandError(err, output)}, &a.pending
	}
	if issue := a.addCatalogConflict(after); issue != "" {
		return result{Name: a.sandbox.Name, Status: "association warning", Detail: issue}, &a.pending
	}
	return a.add()
}

func (a *registrationAttempt) currentResult(fallback string) result {
	value := result{Name: a.sandbox.Name, Status: "stale", Detail: fallback}
	if a.currentError == nil {
		return value
	}
	if err := a.currentError(); err != nil {
		value.Detail = "Registration paused: " + err.Error()
		var unavailable *InventoryUnavailableError
		if errors.As(err, &unavailable) {
			value.Status = "observation unavailable"
			value.DiagnosticCode = store.CodeRegistration
		}
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
