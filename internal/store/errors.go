package store

import (
	"crypto/rand"
	"sort"
	"strings"
	"time"
)

// Severity distinguishes a usable state that needs attention from a blocked action.
type Severity string

const (
	// SeverityInfo marks an operation outcome retained only in event history.
	SeverityInfo Severity = "info"
	// SeverityWarning marks an outcome that leaves the existing machine usable.
	SeverityWarning Severity = "warning"
	// SeverityError marks failed work or status that cannot be trusted.
	SeverityError Severity = "error"
)

// SeverityForCode classifies known harmless outcomes and treats unknown codes as errors.
func SeverityForCode(code string) Severity {
	if code == CodePreparation {
		return SeverityInfo
	}
	if code == CodeOwnershipConflict || code == CodeSandboxDeletion {
		return SeverityWarning
	}
	return SeverityError
}

const (
	// CodeDaemonUnreachable means a daemon control or stop-confirmation deadline expired.
	CodeDaemonUnreachable = "D0001"
	// CodeDaemonStart means a daemon start request failed.
	CodeDaemonStart = "D0002"
	// CodeDaemonStop means a daemon stop request failed for a reason other than a deadline.
	CodeDaemonStop = "D0003"
	// CodeDaemonStatus means the saved daemon status could not be read.
	CodeDaemonStatus = "D0004"
	// CodeDaemonReconnect means the daemon event stream is reconnecting.
	CodeDaemonReconnect = "D0005"
	// CodeDiagnosticDismiss means saving a diagnostic acknowledgement failed.
	CodeDiagnosticDismiss = "D0006"
	// CodeSandboxInventory means the sandbox daemon or its inventory could not be read.
	CodeSandboxInventory = "S0001"
	// CodeSandboxDeletion means deletion verification failed; the existing registration is retained.
	CodeSandboxDeletion = "S0002"
	// CodeSSHSetup means sandbox SSH setup failed.
	CodeSSHSetup = "H0001"
	// CodeRename means a Herdr machine label could not be changed.
	CodeRename = "H0002"
	// CodeHerdrCompatibility means the guest herdr executable lacks a required API capability.
	CodeHerdrCompatibility = "H0003"
	// CodeHerdrFix means an explicit guest herdr release repair failed.
	CodeHerdrFix = "H0004"
	// CodeRegistration means sandbox registration failed without a more specific outcome.
	CodeRegistration = "R0001"
	// CodeManagement means changing enabled intent or removing an entry failed.
	CodeManagement = "R0002"
	// CodePreparation records a re-sync outcome as an informational event.
	CodePreparation = "R0004"
	// CodeOwnershipConflict means the sandbox-to-entry association is ambiguous.
	CodeOwnershipConflict = "R0005"
	// CodeWorkspaceSetup means project workspace setup failed.
	CodeWorkspaceSetup = "R0006"
	// CodeSandboxSetup means sandbox or guest setup failed.
	CodeSandboxSetup = "R0007"
	// CodeSafeEntryUnsupported means herdr cannot safely create an offline entry.
	CodeSafeEntryUnsupported = "R0008"
	// CodePluginChoice means saving a plugin sync choice failed.
	CodePluginChoice = "P0001"
	// CodeGuestPluginSync means a guest plugin could not be synchronized.
	CodeGuestPluginSync = "P0002"
	// CodeGuestPluginResponse means a guest plugin inspection response was malformed.
	CodeGuestPluginResponse = "P0003"
	// CodePluginDiscovery means host plugin choices or defaults could not be read.
	CodePluginDiscovery = "P0004"
	// CodeSandboxNetwork means sandbox network policy blocked a guest plugin build.
	CodeSandboxNetwork = "P0005"
	// CodeTemporaryAccess means installation with temporary download access failed.
	CodeTemporaryAccess = "T0001"
	// CodePolicyCleanup means temporary download access cleanup failed.
	CodePolicyCleanup = "T0002"
)

// IsGuestPluginCode identifies errors from guest plugin synchronization.
func IsGuestPluginCode(code string) bool {
	return code == CodeGuestPluginSync || code == CodeGuestPluginResponse || code == CodeSandboxNetwork
}

func isRegistrationCode(code string) bool {
	switch code {
	case CodeSafeEntryUnsupported, CodeRegistration, CodeOwnershipConflict, CodeWorkspaceSetup, CodeSandboxSetup, CodeHerdrCompatibility:
		return true
	}
	return false
}

// SetRegistrationError replaces the previous registration outcome for one sandbox.
// An empty code clears it after a confirmed ready result.
func SetRegistrationError(code, scope, detail string) error {
	return UpdateStatusStore(func(s *StatusStore) {
		for key, entry := range s.Errors {
			if entry.Scope == scope && isRegistrationCode(entry.Code) && (entry.Code != code || entry.Detail != detail) {
				delete(s.Errors, key)
			}
		}
		if code != "" {
			recordError(s, code, scope, detail)
		}
	})
}

// SetSandboxDeletionFailure records the current verification cause for one
// sandbox. An empty detail clears its diagnostic and notification suppression
// after the sandbox is confirmed present or absent, independent of cleanup.
// The caller must save this change through UpdateStatusStore.
func (s *StatusStore) SetSandboxDeletionFailure(scope, detail string) {
	for key, entry := range s.Errors {
		if entry.Code == CodeSandboxDeletion && entry.Scope == scope && (detail == "" || entry.Detail != detail) {
			delete(s.Errors, key)
		}
	}
	if detail == "" {
		delete(s.Warnings, scope+"/absence")
		if cached, ok := s.Sandboxes[scope]; ok {
			cached.clearDeletionUncertainty()
			s.Sandboxes[scope] = cached
		}
		return
	}
	recordError(s, CodeSandboxDeletion, scope, detail)
}

// EventRecord retains an operation outcome after its active diagnostic is resolved.
type EventRecord struct {
	Ref      string `json:"ref,omitempty"`
	PluginID string `json:"plugin_id,omitempty"`
	Output   string `json:"output,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Command  string `json:"command,omitempty"`

	Code     string    `json:"code"`
	Scope    string    `json:"scope"`
	Name     string    `json:"name,omitempty"`
	Detail   string    `json:"detail"`
	Resolved bool      `json:"resolved"`
	At       time.Time `json:"at"`
}

// ErrorRecord retains a deduplicated unresolved failure across TUI sessions.
type ErrorRecord struct {
	Ref      string `json:"ref,omitempty"`
	PluginID string `json:"plugin_id,omitempty"`
	Output   string `json:"output,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Command  string `json:"command,omitempty"`

	Dismissed bool      `json:"dismissed,omitempty"`
	Code      string    `json:"code"`
	Scope     string    `json:"scope"`
	Detail    string    `json:"detail"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// PrimaryError returns a deterministic active diagnostic for one sandbox.
// Dismissed diagnostics remain active; errors precede warnings, then plugin IDs
// and codes provide a stable order independent of map iteration.
func PrimaryError(scope string) (ErrorRecord, bool, error) {
	s, err := ReadStatusStore()
	if err != nil {
		return ErrorRecord{}, false, err
	}
	var entries []ErrorRecord
	for _, entry := range s.Errors {
		if entry.Scope == scope {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return diagnosticLess(entries[i], entries[j]) })
	if len(entries) == 0 {
		return ErrorRecord{}, false, nil
	}
	return entries[0], true, nil
}

func diagnosticLess(a, b ErrorRecord) bool {
	if SeverityForCode(a.Code) != SeverityForCode(b.Code) {
		return SeverityForCode(a.Code) == SeverityError
	}
	if a.PluginID != b.PluginID {
		return a.PluginID < b.PluginID
	}
	if a.Code != b.Code {
		return a.Code < b.Code
	}
	if a.Detail != b.Detail {
		return a.Detail < b.Detail
	}
	return a.Ref < b.Ref
}

func newErrorRef(s *StatusStore) string {
	return unusedErrorRef(s, func() string { return strings.ToLower(rand.Text()[:6]) })
}

func unusedErrorRef(s *StatusStore, candidate func() string) string {
	used := make(map[string]bool, len(s.Errors)+len(s.Events))
	for _, entry := range s.Errors {
		used[entry.Ref] = true
	}
	for _, event := range s.Events {
		used[event.Ref] = true
	}
	for {
		if ref := candidate(); !used[ref] {
			return ref
		}
	}
}

// Older diagnostics gain durable references on the next journal update.
func ensureErrorRefs(s *StatusStore) {
	for key, entry := range s.Errors {
		if entry.Ref == "" {
			entry.Ref = newErrorRef(s)
			s.Errors[key] = entry
		}
	}
}

func errorGroupKey(scope, code string) string { return scope + "\x00" + code }

// normalizeErrorDismissals keeps an acknowledgement until the entire group
// recovers. Run after a complete journal transaction, so replacing its members
// does not briefly clear the acknowledgement.
func normalizeErrorDismissals(s *StatusStore) {
	if s.DismissedErrorGroups == nil {
		s.DismissedErrorGroups = map[string]bool{}
	}
	allDismissed := dismissedGroups(s.Errors)
	for key := range s.DismissedErrorGroups {
		if _, active := allDismissed[key]; !active {
			delete(s.DismissedErrorGroups, key)
		}
	}
	for key, dismissed := range allDismissed {
		// Import older per-entry acknowledgements only when every member was
		// dismissed. A mixed group must remain visible in New.
		if dismissed {
			s.DismissedErrorGroups[key] = true
		}
	}
	for key, entry := range s.Errors {
		entry.Dismissed = s.DismissedErrorGroups[errorGroupKey(entry.Scope, entry.Code)]
		s.Errors[key] = entry
	}
}

func dismissedGroups(entries map[string]ErrorRecord) map[string]bool {
	allDismissed := map[string]bool{}
	for _, entry := range entries {
		key := errorGroupKey(entry.Scope, entry.Code)
		previous, found := allDismissed[key]
		allDismissed[key] = entry.Dismissed && (!found || previous)
	}
	return allDismissed
}

// DismissError acknowledges the selected diagnostic's group without resolving it.
// A stale selection cannot dismiss a replacement cause or lifecycle.
// New members stay dismissed until every member of this group has recovered.
func DismissError(selected ErrorRecord) error {
	return UpdateStatusStore(func(s *StatusStore) {
		for _, entry := range s.Errors {
			if entry.Code == selected.Code && entry.Scope == selected.Scope && entry.PluginID == selected.PluginID && entry.Detail == selected.Detail && entry.Output == selected.Output && entry.Recovery == selected.Recovery && entry.Command == selected.Command && entry.FirstSeen.Equal(selected.FirstSeen) && (selected.Ref == "" || entry.Ref == selected.Ref) {
				s.DismissedErrorGroups[errorGroupKey(entry.Scope, entry.Code)] = true
				return
			}
		}
	})
}

// RecordError counts identical causes without hiding unrelated failures.
func RecordError(code, scope, detail string) error {
	return UpdateStatusStore(func(s *StatusStore) {
		recordError(s, code, scope, detail)
	})
}

func recordError(s *StatusStore, code, scope, detail string) {
	if SeverityForCode(code) == SeverityInfo {
		appendEvent(s, EventRecord{Code: code, Scope: scope, Detail: detail, At: time.Now()})
		return
	}
	key := code + "\x00" + scope + "\x00" + detail
	entry := s.Errors[key]
	if entry.Ref == "" {
		entry.Ref = newErrorRef(s)
	}
	entry.Code = code
	entry.Scope = scope
	entry.Detail = detail
	entry.Count++
	entry.LastSeen = time.Now()
	if entry.FirstSeen.IsZero() {
		entry.FirstSeen = entry.LastSeen
	}
	s.Errors[key] = entry
}

// PluginFailure describes one plugin outcome without changing sandbox navigation scope.
type PluginFailure struct {
	PluginID   string    `json:"plugin_id,omitempty"`
	Code       string    `json:"code"`
	Detail     string    `json:"detail"`
	Output     string    `json:"output,omitempty"`
	Recovery   string    `json:"recovery,omitempty"`
	Command    string    `json:"command,omitempty"`
	OccurredAt time.Time `json:"occurred_at,omitempty"`
}

// SetGuestPluginFailures records the complete observed plugin result for a sandbox.
// Previously failed plugins absent from this result get independent recovery events.
func SetGuestPluginFailures(scope string, failures []PluginFailure) error {
	return UpdateStatusStore(func(s *StatusStore) {
		remaining := map[string]bool{}
		for _, failure := range failures {
			remaining[failure.PluginID] = true
		}
		for key, entry := range s.Errors {
			if IsGuestPluginCode(entry.Code) && entry.Scope == scope && !remaining[entry.PluginID] {
				delete(s.Errors, key)
				appendEvent(s, EventRecord{Ref: entry.Ref, Code: entry.Code, Scope: scope, PluginID: entry.PluginID, Detail: "Guest plugin sync recovered", Resolved: true, At: time.Now()})
			}
		}
		for _, failure := range failures {
			recordPluginFailure(s, scope, failure)
		}
	})
}

func recordPluginFailure(s *StatusStore, scope string, failure PluginFailure) {
	if !IsGuestPluginCode(failure.Code) {
		failure.Code = CodeGuestPluginSync
	}
	previous := takePluginFailure(s, scope, failure.PluginID)
	same := previous.Code == failure.Code && previous.Detail == failure.Detail && previous.Output == failure.Output && previous.Command == failure.Command && previous.Recovery == failure.Recovery
	at := failure.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	entry := ErrorRecord{Code: failure.Code, Scope: scope, PluginID: failure.PluginID, Detail: failure.Detail, Output: failure.Output, Recovery: failure.Recovery, Command: failure.Command, Count: 1, FirstSeen: at, LastSeen: at}
	if same {
		entry.Ref = previous.Ref
		entry.Dismissed = previous.Dismissed
		entry.FirstSeen = previous.FirstSeen
		entry.Count = previous.Count
		entry.LastSeen = previous.LastSeen
		// Reobserving one saved build attempt is not another failed attempt.
		if failure.OccurredAt.IsZero() || !at.Equal(previous.LastSeen) {
			entry.Count++
			entry.LastSeen = at
		}
	} else {
		entry.Ref = newErrorRef(s)
		appendEvent(s, EventRecord{Ref: entry.Ref, Code: entry.Code, Scope: scope, PluginID: entry.PluginID, Detail: entry.Detail, Output: entry.Output, Recovery: entry.Recovery, Command: entry.Command, At: at})
	}
	key := entry.Code + "\x00" + scope + "\x00" + entry.PluginID + "\x00" + entry.Detail
	s.Errors[key] = entry
}

func takePluginFailure(s *StatusStore, scope, pluginID string) ErrorRecord {
	var previous ErrorRecord
	for key, entry := range s.Errors {
		if !IsGuestPluginCode(entry.Code) || entry.Scope != scope || entry.PluginID != pluginID {
			continue
		}
		if entry.LastSeen.After(previous.LastSeen) {
			previous = entry
		}
		delete(s.Errors, key)
	}
	return previous
}

// RecordPolicyCleanupChecked retains a successful temporary access cleanup check.
func RecordPolicyCleanupChecked(scope string) error {
	return UpdateStatusStore(func(s *StatusStore) {
		appendEvent(s, EventRecord{Code: CodePolicyCleanup, Scope: scope, Name: "All sandboxes", Detail: "Temporary access cleanup checked", Resolved: true, At: time.Now()})
	})
}

// RecordGuestPluginFailure saves an unresolved failure and one history event
// when its detail changes. Repeated scans update the error count only.
func RecordGuestPluginFailure(scope, detail string) error {
	return RecordGuestPluginFailureCode(CodeGuestPluginSync, scope, detail)
}

// RecordGuestPluginFailureCode records a specific guest plugin failure and transition.
func RecordGuestPluginFailureCode(code, scope, detail string) error {
	return UpdateStatusStore(func(s *StatusStore) {
		recordPluginFailure(s, scope, PluginFailure{Code: code, Detail: detail})
	})
}

// ClearGuestPluginFailure resolves the active failure and retains a recovery
// event. A scan that was already healthy does not create another event.
func ClearGuestPluginFailure(scope string) error {
	return SetGuestPluginFailures(scope, nil)
}

func appendEvent(s *StatusStore, event EventRecord) {
	if event.Name == "" {
		event.Name = s.Sandboxes[event.Scope].Name
	}
	s.Events = append(s.Events, event)
	retainEvents(s, time.Now())
}

// normalizeEvents moves informational outcomes out of active diagnostics and
// expires history even when the journal has not received a new event.
func normalizeEvents(s *StatusStore) {
	for key, entry := range s.Errors {
		if SeverityForCode(entry.Code) == SeverityInfo {
			appendEvent(s, EventRecord{Ref: entry.Ref, Code: entry.Code, Scope: entry.Scope, Detail: entry.Detail, At: entry.LastSeen})
			delete(s.Errors, key)
		}
	}
	retainEvents(s, time.Now())
}

func retainEvents(s *StatusStore, now time.Time) {
	cutoff := now.Add(-time.Hour)
	events := s.Events[:0]
	for _, event := range s.Events {
		if event.At.After(cutoff) {
			events = append(events, event)
		}
	}
	if len(events) > 50 {
		order := make([]int, len(events))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool { return events[order[i]].At.Before(events[order[j]].At) })
		order = order[len(order)-50:]
		// Retain the newest timestamps, but preserve history's append order.
		// A late saved attempt must not move after its recovery event.
		sort.Ints(order)
		for i, index := range order {
			events[i] = events[index]
		}
		events = events[:50]
	}
	s.Events = events
}

// ClearError resolves an operation failure after a confirming recovery result.
// Per-plugin failures clear only through a complete guest plugin observation.
func ClearError(code, scope string) error {
	return UpdateStatusStore(func(s *StatusStore) {
		for key, entry := range s.Errors {
			if entry.Code == code && entry.Scope == scope && entry.PluginID == "" {
				delete(s.Errors, key)
			}
		}
	})
}
