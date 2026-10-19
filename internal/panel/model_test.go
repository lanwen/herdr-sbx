package panel

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func keyMsg(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }
func modelFixture(t *testing.T) *model {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m := newModel(nil)
	m.loading = false
	m.rows = []registration.SandboxRow{{Sandbox: sbx.Sandbox{ID: "a", Name: "alpha", Status: "running"}, Scope: "scope", Management: store.Watched, Owned: true, Label: "API desk"}, {Sandbox: sbx.Sandbox{ID: "b", Name: "beta", Status: "stopped"}, Scope: "scope", Management: store.Unmanaged, Label: "Release desk"}}
	m.ensureSelection()
	return m
}
func TestExpansionSelectionAndFiltering(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.expanded != "a" {
		t.Fatal(m.expanded)
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.expanded != "b" {
		t.Fatal(m.expanded)
	}
	m.key(keyMsg('/', "/"))
	m.key(keyMsg('A', "API"))
	if m.selected != "a" || m.expanded != "b" || len(m.visibleRows()) != 1 {
		t.Fatalf("selection=%s expansion=%s", m.selected, m.expanded)
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.queries["Sandboxes"] != "API" {
		t.Fatal("Escape discarded filter")
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.queries["Sandboxes"] != "" {
		t.Fatal("table Escape failed to clear")
	}
}
func TestRenamePreservesTextAcrossRefreshAndFailure(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.input.text = "New desk"
	m.input.cursor = 8
	m.mergeRows([]registration.SandboxRow{m.rows[1], m.rows[0]})
	if m.rows[0].Sandbox.ID != "a" || m.input.text != "New desk" {
		t.Fatal("refresh disturbed editor/order")
	}
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, err: errors.New("name taken"), rename: true})
	if m.input == nil || m.input.text != "New desk" {
		t.Fatal("failed rename discarded text")
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.rows[0].Label != "API desk" {
		t.Fatal("cancel changed label")
	}
}
func TestInputCapturesGlobalShortcuts(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.key(keyMsg('q', "q"))
	m.key(keyMsg('p', "p"))
	if m.page != "Sandboxes" || !strings.HasSuffix(m.input.text, "qp") {
		t.Fatal("global shortcut ran during input")
	}
}
func TestInventoryFailureRetainsRowsAndPersistsError(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	_, cmd := m.Update(observationMsg{err: errors.New("daemon unavailable")})
	runCommands(cmd)
	if len(m.rows) != 2 || !m.rows[0].Status.Stale {
		t.Fatal("inventory failure removed or refreshed rows")
	}
	saved, err := store.ReadStatusStore()
	if err != nil || len(saved.Errors) != 1 {
		t.Fatalf("error not persisted: %v %#v", err, saved.Errors)
	}
}
func TestStoppedWorkerPromptDefaultsToKeepStopped(t *testing.T) {
	m := modelFixture(t)
	_, cmd := m.Update(workerMsg{initialized: true, status: store.WorkerStatus{State: "stopped"}})
	if cmd != nil || m.dialog != "startup" || m.button != 0 {
		t.Fatal("stopped worker started automatically")
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.dialog != "" {
		t.Fatal("decline failed")
	}
	m.key(keyMsg('p', "p"))
	if m.page != "Plugins" {
		t.Fatal("configuration unavailable while stopped")
	}
}
func TestMouseSelectionAndDisclosureShareLayout(t *testing.T) {
	m := modelFixture(t)
	m.View()
	var selectHit, expandHit hit
	for _, h := range m.hits {
		if h.id == "b" && h.action == "select" {
			selectHit = h
		}
		if h.id == "b" && h.action == "expand" {
			expandHit = h
		}
	}
	m.click(selectHit.x, selectHit.y)
	if m.selected != "b" || m.expanded != "b" {
		t.Fatal("row click did not open details")
	}
	m.click(expandHit.x, expandHit.y)
	if m.expanded != "" {
		t.Fatal("same-row disclosure did not close details")
	}
}
func TestSmallViewPreservesWorkerErrorsAndState(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.selected = "b"
	m.expanded = "b"
	m.addError(store.CodeSSHSetup, "Host", errors.New("missing configuration"))
	view := m.View()
	for _, want := range []string{"Auto-sync OFF", "beta → Release desk", "? help"} {
		if !strings.Contains(view.Content, want) {
			t.Fatalf("missing %q in %s", want, view.Content)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	view = m.View()
	if strings.Contains(view.Content, "Resize") || m.selected != "b" || m.expanded != "b" {
		t.Fatal("resize lost state")
	}
}
func TestPluginOverrideShowsLiveDefaultAndSavedChoice(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", Enabled: true}}
	m.settings = store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"test.plugin": {Enabled: true}}, Overrides: map[string]map[string]bool{store.StatusKey("scope", "a"): {"test.plugin": false}}}
	m.expanded = "a"
	view := m.View()
	if !strings.Contains(view.Content, "Off") || !strings.Contains(view.Content, "test.plugin") {
		t.Fatal(view.Content)
	}
}

func TestArrowSelectionRetainsExpandedDetails(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.key(keyMsg(tea.KeyEnd, ""))
	if m.selected != "b" || m.expanded != "a" {
		t.Fatal("arrow selection closed expanded details")
	}
	m.ensureSelection()
	if m.expanded != "a" {
		t.Fatal("refresh closed surviving expansion")
	}
	m.hitAction(hit{action: "select", id: "b"})
	if m.expanded != "b" {
		t.Fatal("new-row click did not replace details")
	}
	m.hitAction(hit{action: "select", id: "b"})
	if m.expanded != "" {
		t.Fatal("same-row click did not close details")
	}
	m.hitAction(hit{action: "select", id: "a"})
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.expanded != "" {
		t.Fatal("Escape did not close details")
	}
}
func TestMouseCannotNavigateAwayFromActiveInput(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.hitAction(hit{action: "select", id: "b"})
	m.hitAction(hit{action: "page", id: "Plugins"})
	if m.selected != "a" || m.page != "Sandboxes" || m.input == nil {
		t.Fatal("mouse navigated away during input")
	}
}
func TestOldRenameCompletionDoesNotEraseNewInput(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	old := m.input
	m.input = nil
	m.beginRename()
	current := m.input
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeRename, rename: true, input: old})
	if m.input != current {
		t.Fatal("old operation erased new input")
	}
}
func TestCloseWaitsForPendingCleanup(t *testing.T) {
	m := modelFixture(t)
	m.pending["a"] = true
	cmd := m.key(keyMsg('q', "q"))
	if cmd != nil || !m.closing {
		t.Fatal("closed before operation cleanup")
	}
	_, cmd = m.Update(operationMsg{target: "a", code: store.CodeTemporaryAccess})
	if cmd == nil {
		t.Fatal("did not close after cleanup")
	}
}

func runCommands(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			runCommands(child)
		}
	}
}

func TestRemovalErrorRecoverySelectsDisable(t *testing.T) {
	m := modelFixture(t)
	m.errors = []uiError{{code: store.CodeManagement, scope: "a", detail: "entry remains"}}
	m.focusErrors()
	m.activate()
	if m.page != "Sandboxes" || m.selected != "a" || m.expanded != "a" || m.focus != "actions" ||
		m.sandboxActions(m.rows[0])[m.button].id != "disable" {
		t.Fatalf("wrong recovery: page=%s selected=%s focus=%s button=%d", m.page, m.selected, m.focus, m.button)
	}
}
func TestNarrowActiveInputsRemainVisible(t *testing.T) {
	for _, kind := range []string{"rename"} {
		t.Run(kind, func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = 60, 18
			m.expanded = "a"
			m.plugins = make([]guestplugins.Choice, 30)
			m.input = &inputState{kind: kind, target: "a", text: "editable", cursor: 8}
			view := m.View()
			if !strings.Contains(view.Content, "editable│") {
				t.Fatalf("input hidden: %s", view.Content)
			}
		})
	}
}
func TestMouseRenameRevealsInlineEditor(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	m.expanded = "a"
	m.plugins = make([]guestplugins.Choice, 30)
	m.focus = "actions"
	m.hitAction(hit{action: "action", id: "rename"})
	view := m.View()
	if m.input == nil || !strings.Contains(view.Content, "API desk│") {
		t.Fatalf("inline editor hidden: %s", view.Content)
	}
}
func TestErrorKeyboardSelectionRevealsRecoveryTarget(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 60, 18
	for i := 0; i < 10; i++ {
		m.errors = append(m.errors, uiError{code: store.CodeRename, scope: string(rune('a' + i)), detail: strings.Repeat("long diagnostic ", 8)})
	}
	m.focusErrors()
	for i := 0; i < 8; i++ {
		m.key(keyMsg(tea.KeyDown, ""))
	}
	view := m.View()
	if !strings.Contains(view.Content, "> H0002 · i") {
		t.Fatalf("selected error hidden: %s", view.Content)
	}
}

func TestPanelActivationSchedulesPolicyCleanupOnceWithoutWorker(t *testing.T) {
	m := modelFixture(t)
	m.worker = store.WorkerStatus{State: "stopped"}
	m.workerLoaded = true
	_, cmd := m.Update(observationMsg{})
	if m.policyCleanupRequested || cmd != nil {
		t.Fatal("cleanup scheduled without daemon identity")
	}
	observation := observationMsg{checks: precheck.Result{Scope: "scope", Socket: "/tmp/test.sock"}}
	_, cmd = m.Update(observation)
	if cmd == nil || !m.policyCleanupRequested || !m.pending["policy-cleanup"] {
		t.Fatal("stopped worker prevented activation cleanup")
	}
	_, cmd = m.Update(observation)
	if cmd != nil {
		t.Fatal("activation cleanup scheduled more than once")
	}
	m.refreshing = true
	_, cmd = m.Update(operationMsg{target: "policy-cleanup", code: store.CodePolicyCleanup, err: errors.New("rules need manual cleanup")})
	if cmd == nil || len(m.errors) != 1 || m.errors[0].code != store.CodePolicyCleanup {
		t.Fatal("cleanup failure was not surfaced")
	}
}
