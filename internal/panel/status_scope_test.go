package panel

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStatusContainsOnlyGlobalObservations(t *testing.T) {
	for _, width := range []int{110, 40} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			unsetNoColor(t)
			m := statusScopeFixture(t)
			m.width, m.height = width, 80
			m.openTab("Status")
			m.eventsExpanded = true
			frame := m.View().Content
			text := ansi.Strip(frame)
			for _, unwanted := range []string{"Registration association", "entry creation is not confirmed", "Retry Enable", "sandbox-only-error", "sandbox-only-event", "deleted-sandbox-error"} {
				require.NotContains(t, text, unwanted)
			}
			for _, wanted := range []string{"Management", "Sandbox service", "global-only-error", "global-only-event", "saved associations"} {
				require.Contains(t, text, wanted)
			}
			require.NotContains(t, text, "Unknown sandbox")
			require.Contains(t, frame, colorError+store.CodeDaemonStart)
			require.Len(t, m.diagnosticGroups(), 1)
			require.Contains(t, ansi.Strip(m.eventTitle()), "1 recent")
			m.key(keyMsg('e', "e"))
			require.Equal(t, "global-only-error", m.visibleErrors()[m.errorIndex].detail)
			m.openTab("Sandboxes")
			require.Len(t, m.diagnosticGroups(), 3)
			require.Contains(t, ansi.Strip(m.eventTitle()), "2 recent")
			require.Equal(t, "sandbox-only-error", m.rows[0].AssociationIssue)
			m.selected, m.expanded, m.focus = "a", "a", "actions"
			details := ansi.Strip(m.View().Content)
			require.Contains(t, details, "sandbox-only-error")
		})
	}
}

func TestStatusSandboxDiagnosticsDoNotCreateNavigationTargets(t *testing.T) {
	m := statusScopeFixture(t)
	m.errors = m.errors[1:]
	m.events = m.events[1:]
	m.openTab("Status")
	m.key(keyMsg('e', "e"))
	require.Equal(t, "table", m.focus)
	m.button = len(m.statusActions()) - 1
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "table", m.focus)
	require.NotContains(t, ansi.Strip(m.renderHelp(110, true)), "diagnostics")
}

func statusScopeFixture(t *testing.T) *model {
	t.Helper()
	m := modelFixture(t)
	m.rows[0].AssociationIssue = "sandbox-only-error"
	m.checks.Checks = []precheck.Check{
		{Name: "Sandbox daemon", Level: precheck.Ready, Detail: "running"},
		{Name: "Registration state", Level: precheck.Unknown, Detail: "saved associations"},
		{Name: "Registration association", Level: precheck.Unknown, Detail: "entry creation is not confirmed for sbx · alpha", Action: "Retry Enable"},
	}
	at := time.Now()
	m.errors = []uiError{
		{code: store.CodeDaemonStart, scope: "scope/catch-up", detail: "global-only-error", count: 1, firstSeen: at, seen: at},
		{code: store.CodeOwnershipConflict, scope: store.StatusKey("scope", "a"), detail: "sandbox-only-error", count: 1, firstSeen: at, seen: at},
		{code: store.CodeSandboxDeletion, scope: store.StatusKey("scope", "deleted"), detail: "deleted-sandbox-error", count: 1, firstSeen: at, seen: at},
	}
	m.events = []store.EventRecord{
		{Code: store.CodeDaemonStart, Scope: "scope/stream", Detail: "global-only-event", At: at},
		{Code: store.CodePreparation, Scope: store.StatusKey("scope", "a"), Name: "alpha", Detail: "sandbox-only-event", At: at},
	}
	return m
}

func TestPendingAssociationGuidanceStaysWithObservedSandbox(t *testing.T) {
	service := testPanel(t, nil)
	scope := store.ScopeID("/tmp/panel-test.sock")
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "first", Target: "alpha.sbx", Label: "sbx · alpha", Pending: true}}}}))
	rows, err := service.registration.SandboxRows(context.Background(), "/tmp/panel-test.sock")
	require.NoError(t, err)
	m := newModel(service)
	m.rows, m.loading = rows, false
	m.width, m.height = 110, 42
	m.page, m.selected, m.expanded, m.focus = "Sandboxes", "first", "first", "actions"
	frame := ansi.Strip(m.View().Content)
	require.Contains(t, frame, "no saved connection")
	require.Contains(t, frame, "check again automatically")
	require.Contains(t, frame, "alpha")
}

func TestStatusOperationNoticesFollowTheirContext(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 32
	m.actionNotice("a", store.CodeRename)
	m.openTab("Status")
	m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "sandbox-only completion"})
	require.NotContains(t, ansi.Strip(m.View().Content), "sandbox-only completion")
	m.Update(operationMsg{target: "Host", notice: "global-only completion"})
	require.Contains(t, ansi.Strip(m.View().Content), "global-only completion")
	m.openTab("Sandboxes")
	m.Update(operationMsg{target: "a", code: store.CodeRename, notice: "sandbox-only completion"})
	require.Contains(t, ansi.Strip(m.View().Content), "sandbox-only completion")
}

// Capture only simulated observations through the shared screenshot fixture.
func TestCaptureStatusScopeFrames(t *testing.T) {
	dir := os.Getenv("PANEL_STATUS_SCOPE_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set PANEL_STATUS_SCOPE_SCREENSHOT_DIR to capture status scope frames")
	}
	unsetNoColor(t)
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, width := range []int{110, 40} {
		for _, focus := range []string{"management", "diagnostics"} {
			m := statusScopeScreenshot(width)
			if focus == "diagnostics" {
				m.focusErrors()
			}
			path := filepath.Join(dir, "status-global-"+strconv.Itoa(width)+"-"+focus+".ansi")
			require.NoError(t, os.WriteFile(path, []byte(m.View().Content), 0644))
		}
		m := screenshotModel()
		m.width, m.height = width, 42
		m.focus, m.button = "actions", 0
		m.rows[0].EntryPresent, m.rows[0].ProfileID, m.rows[0].Label = false, "", ""
		m.rows[0].Status.Connection = "pending"
		m.rows[0].Status.Detail = "Herdr has no saved connection to this sandbox. The plugin will check again automatically."
		path := filepath.Join(dir, "sandbox-pending-"+strconv.Itoa(width)+".ansi")
		require.NoError(t, os.WriteFile(path, []byte(m.View().Content), 0644))
		m.height = 60
		m.eventsExpanded = true
		m.events = []store.EventRecord{{Ref: "e8d4n2", Code: store.CodeRegistration, Scope: store.StatusKey(m.rows[0].Scope, m.rows[0].Sandbox.ID), Name: m.rows[0].Sandbox.Name, Detail: "machine_add failed: exit status 1", Output: "remote platform detection failed: sandbox temporarily unavailable\nOriginal combined output retained separately from the concise status.", At: time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC)}}
		path = filepath.Join(dir, "registration-evidence-"+strconv.Itoa(width)+".ansi")
		require.NoError(t, os.WriteFile(path, []byte(m.View().Content), 0644))
	}
}

func statusScopeScreenshot(width int) *model {
	m := screenshotModel()
	m.openTab("Status")
	m.width, m.height = width, 42
	m.worker.Detail = "Following sandbox lifecycle events."
	m.checks.Checks = append(m.checks.Checks, precheck.Check{Name: "Registration association", Level: precheck.Unknown, Detail: "entry creation is not confirmed for sbx · " + m.rows[0].Sandbox.Name, Action: "Check the herdr machine catalog. Retry Enable if this sandbox has no entry."})
	at := time.Date(2026, 11, 3, 13, 30, 0, 0, time.UTC)
	scope := store.StatusKey(m.rows[0].Scope, m.rows[0].Sandbox.ID)
	m.errors = []uiError{
		{ref: "s3k8r2", code: store.CodeOwnershipConflict, scope: scope, detail: "Sandbox entry creation is not confirmed.", count: 1, firstSeen: at, seen: at},
		{ref: "g6n2w8", code: store.CodeDaemonReconnect, scope: "examples/stream", detail: "The sandbox event stream was interrupted. Management is reconnecting.", count: 1, firstSeen: at, seen: at},
	}
	m.events = []store.EventRecord{
		{Code: store.CodePreparation, Scope: scope, Name: m.rows[0].Sandbox.Name, Detail: "Sandbox setup requires attention.", At: at},
		{Code: store.CodeDaemonReconnect, Scope: "examples/stream", Detail: "The sandbox event stream was interrupted.", At: at},
	}
	m.eventsExpanded = true
	return m
}
