package panel

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// diagnosticGroup is one focus and dismissal target, with independent plugin causes.
type diagnosticGroup struct {
	uiError
	entries []uiError
}

func diagnosticGroupKey(code, scope string) string { return errorScopeKey(code, scope, "*") }

func (m *model) diagnosticGroups() []diagnosticGroup {
	var groups []diagnosticGroup
	indices := map[string]int{}
	for _, e := range m.errors {
		key := diagnosticGroupKey(e.code, e.scope)
		index, ok := indices[key]
		if !ok {
			index = len(groups)
			indices[key] = index
			groups = append(groups, diagnosticGroup{uiError: e})
			groups[index].count = 0
		}
		group := &groups[index]
		group.entries = append(group.entries, e)
		group.count += max(1, e.count)
		if e.seen.After(group.seen) {
			group.seen = e.seen
		}
		// A persisted group acknowledgement applies to every child, including a new
		// plugin observed while an optimistic dismissal is still being saved.
		group.dismissed = group.dismissed || e.dismissed
	}
	// Keep positions independent of which plugin most recently failed or recovered.
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		an, bn := strings.ToLower(m.diagnosticScope(a.scope, "")), strings.ToLower(m.diagnosticScope(b.scope, ""))
		if an != bn {
			return an < bn
		}
		if a.code != b.code {
			return a.code < b.code
		}
		return a.scope < b.scope
	})
	return groups
}

// visibleErrors retains active groups while New hides acknowledged groups.
func (m *model) visibleErrors() []diagnosticGroup {
	var result []diagnosticGroup
	for _, group := range m.diagnosticGroups() {
		if m.showAllErrors || !group.dismissed {
			result = append(result, group)
		}
	}
	return result
}

func (m *model) toggleDiagnostics() {
	m.showAllErrors = !m.showAllErrors
	m.errorIndex = min(m.errorIndex, max(0, len(m.visibleErrors())-1))
	m.focus, m.reveal = "diagnostics", true
}

func (m *model) dismissError() tea.Cmd {
	entries := m.visibleErrors()
	if m.focus != "errors" || len(entries) == 0 {
		return nil
	}
	selected := entries[min(m.errorIndex, len(entries)-1)]
	if selected.dismissed {
		return nil
	}
	if selected.firstSeen.IsZero() {
		m.notice, m.noticeTarget = "Saving this diagnostic. Try Dismiss again in a moment.", ""
		return nil
	}
	for i := range m.errors {
		e := &m.errors[i]
		if e.code == selected.code && e.scope == selected.scope {
			e.dismissed = true
		}
	}
	m.queueDiagnosticWrite(selected.uiError, func() error {
		record := store.ErrorRecord{Ref: selected.ref, Code: selected.code, Scope: selected.scope, PluginID: selected.pluginID, Detail: selected.detail, FirstSeen: selected.firstSeen, Output: selected.output, Recovery: selected.recovery, Command: selected.command}
		return store.DismissError(record)
	})
	m.errorIndex = min(m.errorIndex, max(0, len(m.visibleErrors())-1))
	if len(m.visibleErrors()) == 0 {
		m.focus = "diagnostics"
	}
	m.reveal = true
	return nil
}

func (m *model) queueDiagnosticWrite(e uiError, apply func() error) {
	if m.errorChanges == nil {
		m.errorChanges = map[string]errorChange{}
	}
	m.errorRevision++
	key := diagnosticGroupKey(e.code, e.scope)
	m.errorChanges[key] = errorChange{revision: m.errorRevision}
	m.effects = append(m.effects, errorWrite{key: key, revision: m.errorRevision, apply: apply, dismissal: &e})
}

func (m *model) focusFollowingSections() {
	if len(m.events) > 0 {
		m.focus, m.eventIndex = "events", -1
		m.reveal = true
		return
	}
	if len(m.errors) > 0 {
		m.focus, m.reveal = "diagnostics", true
	}
}

func (m *model) eventTitle() string {
	arrow := "▸"
	if m.eventsExpanded {
		arrow = "▾"
	}
	if m.focus == "events" && m.eventIndex < 0 {
		arrow = "> " + arrow
	}
	return fmt.Sprintf("%s Events · %d recent", arrow, len(m.events))
}

func diagnosticTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " · " + ink(colorMuted, at.Local().Format("2006-01-02 15:04:05"))
}

func diagnosticIdentity(e uiError) string {
	return errorScopeKey(e.code, e.scope, e.pluginID) + "\x00" + e.detail + "\x00" + e.output + "\x00" + e.recovery + "\x00" + e.command + "\x00" + e.firstSeen.Format(time.RFC3339Nano)
}
func sameDiagnostic(a, b uiError) bool { return diagnosticIdentity(a) == diagnosticIdentity(b) }
