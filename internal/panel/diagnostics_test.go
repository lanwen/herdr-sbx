package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticPluginNameIsBold(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "example.plugin", detail: "Build failed"}}
	m.focusErrors()
	view := m.View().Content
	require.Contains(t, view, "\x1b[1mexample.plugin\x1b[22m", "plugin name is not bold in Errors: %q", view)
	t.Setenv("NO_COLOR", "1")
	view = m.View().Content
	require.Contains(t, view, "example.plugin")
	require.NotContains(t, view, "\x1b[")
}

func TestEventCodeColorsFollowOutcome(t *testing.T) {
	unsetNoColor(t)
	for _, scenario := range []struct {
		name, code, color string
		resolved          bool
	}{
		{"informational", store.CodePreparation, colorInfo, false},
		{"warning", store.CodeOwnershipConflict, colorWarning, false},
		{"error", store.CodeGuestPluginSync, colorError, false},
		{"unknown", "Z9999", colorError, false},
		{"recovered", store.CodeGuestPluginSync, colorSuccess, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m := modelFixture(t)
			m.events = []store.EventRecord{{Code: scenario.code, Scope: "a", Resolved: scenario.resolved}}

			header := m.eventLines()[1].text
			require.Contains(t, header, ink(scenario.color, scenario.code), "incorrect event code color: %q", header)
		})
	}
}

func TestDiagnosticSeverityAndRefresh(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.workerEnabled = true
	m.worker.State, m.worker.LastSync = "running", time.Now()
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: "b", detail: "Manual machine remains usable"}}
	{
		title := m.diagnosticTitle()
		require.True(t, strings.HasPrefix(ansi.Strip(title), "Warnings · New (1) | All (1)"), title)
	}
	for _, action := range m.statusActions() {
		require.NotEqual(t, "refresh", action.id, "a warning alone made Refresh visible")
	}
	warning := m.errorLines()[1].text
	require.Contains(t, warning, ink(colorWarning, "R0005"), "warning label and color missing: %q", warning)
	m.errors = append(m.errors, uiError{code: store.CodeGuestPluginSync, scope: "a"}, uiError{code: "Z9999", scope: "Host"})
	{
		title := m.diagnosticTitle()
		require.Equal(t, "Errors and warnings · New (3) | All (3)", ansi.Strip(title), title)
	}
	var diagnostics strings.Builder
	for _, line := range m.errorLines() {
		diagnostics.WriteString(line.text + "\n")
	}
	require.True(t, m.hasBlockingErrors())
	require.Contains(t, diagnostics.String(), ink(colorError, "P0002"))
	require.Contains(t, diagnostics.String(), ink(colorError, "Z9999"))
	t.Setenv("NO_COLOR", "1")
	m.focusErrors()
	view := m.View().Content
	require.Contains(t, view, "Errors and warnings")
	require.Contains(t, view, "R0005")
	for _, line := range strings.Split(view, "\n") {
		require.False(t, ansi.StringWidth(line) > m.width, "narrow diagnostics exceeded width: %q", line)
	}
}

func TestAssociationWarningOpensAffectedSandbox(t *testing.T) {
	m := modelFixture(t)
	m.rows[1].AssociationIssue = "Two herdr entries match this sandbox. Resolve the duplicate entry."
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: store.StatusKey("scope", "b")}}
	m.key(keyMsg('e', "e"))
	m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "table", m.focus)
	require.Len(t, m.sandboxActions(m.rows[1]), 0)

	hint, _ := m.contextHint()
	require.Equal(t, m.rows[1].AssociationIssue, hint, hint)
}

func TestSandboxHintFollowsSelectionAndDiagnosticsFocus(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].Status.Detail = "already registered"
	m.selected = "b"
	m.rows[1].AssociationIssue = "Duplicate matching entries"
	{
		hint, color := m.contextHint()
		require.Contains(t, hint, "Duplicate matching entries")
		require.False(t, color != colorWarning)
	}
	m.selected = "a"
	{
		hint, color := m.contextHint()
		require.EqualValues(t, "This sandbox is registered in herdr. Setup is complete.", hint)
		require.False(t, color != colorSuccess)
	}
	m.focus = "errors"
	{
		hint, _ := m.contextHint()
		require.Equal(t, "", hint, "sandbox hint remained in diagnostics: %q", hint)
	}
}

func TestEventScopeAndSeverityPresentation(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 60
	m.events = []store.EventRecord{
		{Code: store.CodeGuestPluginSync, Scope: store.StatusKey("scope", "a"), Name: "old alpha", At: time.Now()},
		{Code: store.CodeOwnershipConflict, Scope: "missing/b", Name: "deleted sandbox", At: time.Now()},
		{Code: store.CodeGuestPluginSync, Scope: "long-internal-scope/23eb05ab-e523-4f56-9c11-a72641759", Resolved: true, At: time.Now()},
	}
	var text strings.Builder
	for _, line := range m.eventLines() {
		text.WriteString(line.text + "\n")
		require.False(t, ansi.StringWidth(line.text) > m.width-4, "event exceeded narrow content width: %q", line.text)
	}
	rendered := text.String()
	for _, want := range []string{ink(colorError, "P0002"), ink(colorWarning, "R0005"), ink(colorSuccess, "P0002"), "alpha", "deleted sandbox", "Unknown sandbox (23eb05ab-e5…)"} {
		require.Contains(t, rendered, want, "event missing %q: %s", want, rendered)
	}
	require.NotContains(t, rendered, "long-internal-scope")
	require.NotContains(t, rendered, "old alpha")
	require.Equal(t, store.StatusKey("scope", "a"), m.events[0].Scope, "display changed raw recovery scope")
}

func TestDiagnosticScopesUseExactSandboxIdentity(t *testing.T) {
	m := modelFixture(t)
	for _, test := range []struct{ scope, want string }{
		{store.StatusKey("scope", "a"), "alpha"},
		{"b", "beta"},
		{"Host", "Host"},
		{"daemon", "daemon"},
		{"scope", "scope"},
		{"scope/1234567890123456", "Unknown sandbox (12345678901…)"},
	} {
		{
			got := m.diagnosticScope(test.scope, "")
			require.Equal(t, test.want, got, "scope %q = %q; want %q", test.scope, got, test.want)
		}
	}
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: store.StatusKey("scope", "b")}}
	{
		got := ansi.Strip(m.errorLines()[1].text)
		require.Contains(t, got, "R0005 · beta", "active warning did not show sandbox name: %s", got)
	}
	require.Equal(t, store.StatusKey("scope", "b"), m.errors[0].scope, "presentation changed recovery identity")
}

func TestPluginDiagnosticsKeepRepairsSeparateAtNarrowWidth(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 60, 50
	command := "herdr plugin install 'owner/plugin' --ref '444c12f51fa8f712588cd1481dcd5459f658eac9' --yes"
	m.mergeErrors(workerMsg{errors: map[string]store.ErrorRecord{
		"first":  {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "first.plugin", Detail: "Manual intervention required", Output: "first build failed", Recovery: "Repair this herdr plugin, then choose Re-sync.", Command: command, Count: 1},
		"second": {Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "second.plugin", Detail: "Manual intervention required", Output: "second build failed", Recovery: "Repair the retained source inside this sandbox, then choose Re-sync.", Count: 3},
	}})
	var rendered strings.Builder
	for _, l := range m.errorLines() {
		rendered.WriteString(l.text + "\n")
		require.False(t, ansi.StringWidth(l.text) > m.width-4, "diagnostic exceeds content width: %q", l.text)
	}
	text := ansi.Strip(rendered.String())
	for _, want := range []string{"P0002 · alpha ×4", "first.plugin", "second.plugin", "×4", "Run inside alpha:", "Repair the retained source", "first build failed", "second build failed"} {
		require.Contains(t, text, want, "missing %q: %s", want, text)
	}
	require.EqualValues(t, 1, strings.Count(text, "Run inside"))
	require.EqualValues(t, 1, strings.Count(text, "herdr plugin install"))
	for _, plugin := range []string{"first.plugin", "second.plugin"} {
		_, after, found := strings.Cut(text, plugin)
		require.True(t, found)
		rest := plugin + after
		require.False(t, strings.Index(rest, "│") > strings.Index(rest, "Build output:"), "repair advice follows build output: %s", rest)
	}
	require.Contains(t, rendered.String(), ink(colorAccent, "│"))
	require.Contains(t, rendered.String(), ink(colorMuted, "first build failed"))
	var commandLines []string
	for _, l := range m.repairCallout("alpha", "", command) {
		part := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(l.text), "  │  "))
		if part != "" && part != "Run inside alpha:" {
			commandLines = append(commandLines, part)
		}
	}

	got := strings.Join(commandLines, " ")
	require.Equal(t, command, got, "wrapped command changed: %q", got)

	m.focusErrors()
	for index := range m.visibleErrors() {
		m.errorIndex = index
		m.recoverError()
		require.EqualValues(t, "a", m.selected)
		require.EqualValues(t, "a", m.expanded)
		require.EqualValues(t, "actions", m.focus)
		require.EqualValues(t, "resync", m.sandboxActions(m.rows[0])[m.button].id)
	}
}

func TestPluginRepairCalloutSupportsDirectDrag(t *testing.T) {
	m := modelFixture(t)
	m.width = 60
	command := "herdr plugin install 'owner/plugin' --yes"
	callout := m.repairCallout("alpha", "", command)
	var row string
	for _, l := range callout {
		if strings.Contains(l.text, "herdr plugin install") {
			row = l.text
		}
	}
	require.NotEqual(t, "", row, "command not visible without selecting an entry")
	m.renderedFrame = row
	m.Update(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 5 + len(command) - 1, Y: 0, Button: tea.MouseLeft})
	_, clipboard := m.Update(tea.MouseReleaseMsg{X: 5 + len(command) - 1, Y: 0, Button: tea.MouseLeft})
	assertClipboard(t, clipboard, command)

	got := ansi.Strip(m.View().Content)
	require.Equal(t, ansi.Strip(row), got, "drag changed the visible repair command")
}

func TestPluginEventKeepsRepairAndRawOutputSeparate(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 60
	m.events = []store.EventRecord{{Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "first.plugin", Detail: "Build failed", Output: "raw compiler output", Recovery: "Repair the source inside this sandbox, then choose Re-sync.", At: time.Now()}}
	var rendered strings.Builder
	for _, l := range m.eventLines() {
		rendered.WriteString(l.text + "\n")
		require.False(t, ansi.StringWidth(l.text) > m.width-4, "event exceeds content width: %q", l.text)
	}
	text := rendered.String()
	require.Contains(t, text, "first.plugin")
	require.Contains(t, text, ink(colorError, "P0002"))
	require.False(t, strings.Index(text, "Repair the source") > strings.Index(text, "raw compiler output"))
	require.Contains(t, text, ink(colorMuted, "raw compiler output"))
}
