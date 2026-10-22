package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestDiagnosticPluginNameIsBold(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", pluginID: "example.plugin", detail: "Build failed"}}
	m.focusErrors()
	view := m.View().Content
	if !strings.Contains(view, "\x1b[1mexample.plugin\x1b[22m") {
		t.Fatalf("plugin name is not bold in Errors: %q", view)
	}
	t.Setenv("NO_COLOR", "1")
	view = m.View().Content
	if !strings.Contains(view, "example.plugin") || strings.Contains(view, "\x1b[") {
		t.Fatalf("unstyled Errors lost its plugin name or retained styling: %q", view)
	}
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
			if header := m.eventLines()[1].text; !strings.Contains(header, ink(scenario.color, scenario.code)) {
				t.Fatalf("incorrect event code color: %q", header)
			}
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
	if title := m.diagnosticTitle(); !strings.HasPrefix(ansi.Strip(title), "Warnings · New (1) | All (1)") {
		t.Fatal(title)
	}
	for _, action := range m.statusActions() {
		if action.id == "refresh" {
			t.Fatal("a warning alone made Refresh visible")
		}
	}
	warning := m.errorLines()[1].text
	if !strings.Contains(warning, ink(colorWarning, "R0005")) {
		t.Fatalf("warning label and color missing: %q", warning)
	}
	m.errors = append(m.errors, uiError{code: store.CodeGuestPluginSync, scope: "a"}, uiError{code: "Z9999", scope: "Host"})
	if title := m.diagnosticTitle(); ansi.Strip(title) != "Errors and warnings · New (3) | All (3)" {
		t.Fatal(title)
	}
	var diagnostics strings.Builder
	for _, line := range m.errorLines() {
		diagnostics.WriteString(line.text + "\n")
	}
	if !m.hasBlockingErrors() || !strings.Contains(diagnostics.String(), ink(colorError, "P0002")) || !strings.Contains(diagnostics.String(), ink(colorError, "Z9999")) {
		t.Fatal("blocking or unknown error lost its severity")
	}
	t.Setenv("NO_COLOR", "1")
	m.focusErrors()
	view := m.View().Content
	if !strings.Contains(view, "Errors and warnings") || !strings.Contains(view, "R0005") {
		t.Fatalf("severity is not readable without color: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("narrow diagnostics exceeded width: %q", line)
		}
	}
}

func TestAssociationWarningOpensAffectedSandbox(t *testing.T) {
	m := modelFixture(t)
	m.rows[1].AssociationIssue = "Two herdr entries match this sandbox. Resolve the duplicate entry."
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: store.StatusKey("scope", "b")}}
	m.key(keyMsg('e', "e"))
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.selected != "b" || m.expanded != "b" || m.focus != "table" || len(m.sandboxActions(m.rows[1])) != 0 {
		t.Fatal("unsafe association recovery")
	}
	if hint, _ := m.contextHint(); hint != m.rows[1].AssociationIssue {
		t.Fatal(hint)
	}
}

func TestSandboxHintFollowsSelectionAndDiagnosticsFocus(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].Status.Detail = "already registered"
	m.selected = "b"
	m.rows[1].AssociationIssue = "Duplicate matching entries"
	if hint, color := m.contextHint(); !strings.Contains(hint, "Duplicate matching entries") || color != colorWarning {
		t.Fatalf("hint followed expanded row instead of selection: %q", hint)
	}
	m.selected = "a"
	if hint, color := m.contextHint(); hint != "This sandbox is registered in herdr. Setup is complete." || color != colorSuccess {
		t.Fatalf("hint did not follow selection: %q", hint)
	}
	m.focus = "errors"
	if hint, _ := m.contextHint(); hint != "" {
		t.Fatalf("sandbox hint remained in diagnostics: %q", hint)
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
		if ansi.StringWidth(line.text) > m.width-4 {
			t.Fatalf("event exceeded narrow content width: %q", line.text)
		}
	}
	rendered := text.String()
	for _, want := range []string{ink(colorError, "P0002"), ink(colorWarning, "R0005"), ink(colorSuccess, "P0002"), "alpha", "deleted sandbox", "Unknown sandbox (23eb05ab-e5…)"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("event missing %q: %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "long-internal-scope") || strings.Contains(rendered, "old alpha") {
		t.Fatalf("event displayed internal scope or stale name: %s", rendered)
	}
	if m.events[0].Scope != store.StatusKey("scope", "a") {
		t.Fatal("display changed raw recovery scope")
	}
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
		if got := m.diagnosticScope(test.scope, ""); got != test.want {
			t.Fatalf("scope %q = %q; want %q", test.scope, got, test.want)
		}
	}
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: store.StatusKey("scope", "b")}}
	if got := ansi.Strip(m.errorLines()[1].text); !strings.Contains(got, "R0005 · beta") {
		t.Fatalf("active warning did not show sandbox name: %s", got)
	}
	if m.errors[0].scope != store.StatusKey("scope", "b") {
		t.Fatal("presentation changed recovery identity")
	}
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
		if ansi.StringWidth(l.text) > m.width-4 {
			t.Fatalf("diagnostic exceeds content width: %q", l.text)
		}
	}
	text := ansi.Strip(rendered.String())
	for _, want := range []string{"P0002 · alpha ×4", "first.plugin", "second.plugin", "×4", "Run inside alpha:", "Repair the retained source", "first build failed", "second build failed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, "Run inside") != 1 || strings.Count(text, "herdr plugin install") != 1 {
		t.Fatalf("manual-only repair gained an unverified command: %s", text)
	}
	for _, plugin := range []string{"first.plugin", "second.plugin"} {
		index := strings.Index(text, plugin)
		rest := text[index:]
		if strings.Index(rest, "│") > strings.Index(rest, "Build output:") {
			t.Fatalf("repair advice follows build output: %s", rest)
		}
	}
	if !strings.Contains(rendered.String(), ink(colorAccent, "│")) || !strings.Contains(rendered.String(), ink(colorMuted, "first build failed")) {
		t.Fatal("repair callout or raw output lacks its style")
	}
	var commandLines []string
	for _, l := range m.repairCallout("alpha", "", command) {
		part := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(l.text), "  │  "))
		if part != "" && part != "Run inside alpha:" {
			commandLines = append(commandLines, part)
		}
	}
	if got := strings.Join(commandLines, " "); got != command {
		t.Fatalf("wrapped command changed: %q", got)
	}
	m.focusErrors()
	for index := range m.visibleErrors() {
		m.errorIndex = index
		m.recoverError()
		if m.selected != "a" || m.expanded != "a" || m.focus != "actions" || m.sandboxActions(m.rows[0])[m.button].id != "resync" {
			t.Fatal("plugin diagnostic did not select sandbox Re-sync")
		}
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
	if row == "" {
		t.Fatal("command not visible without selecting an entry")
	}
	m.renderedFrame = row
	m.Update(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 5 + len(command) - 1, Y: 0, Button: tea.MouseLeft})
	_, clipboard := m.Update(tea.MouseReleaseMsg{X: 5 + len(command) - 1, Y: 0, Button: tea.MouseLeft})
	assertClipboard(t, clipboard, command)
	if got := ansi.Strip(m.View().Content); got != ansi.Strip(row) {
		t.Fatal("drag changed the visible repair command")
	}
}

func TestPluginEventKeepsRepairAndRawOutputSeparate(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 60
	m.events = []store.EventRecord{{Code: store.CodeGuestPluginSync, Scope: "a", PluginID: "first.plugin", Detail: "Build failed", Output: "raw compiler output", Recovery: "Repair the source inside this sandbox, then choose Re-sync.", At: time.Now()}}
	var rendered strings.Builder
	for _, l := range m.eventLines() {
		rendered.WriteString(l.text + "\n")
		if ansi.StringWidth(l.text) > m.width-4 {
			t.Fatalf("event exceeds content width: %q", l.text)
		}
	}
	text := rendered.String()
	if !strings.Contains(text, "first.plugin") || !strings.Contains(text, ink(colorError, "P0002")) || strings.Index(text, "Repair the source") > strings.Index(text, "raw compiler output") || !strings.Contains(text, ink(colorMuted, "raw compiler output")) {
		t.Fatalf("event lost repair context or severity: %s", text)
	}
}
