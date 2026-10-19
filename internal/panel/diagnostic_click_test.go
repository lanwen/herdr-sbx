package panel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func clickDiagnostic(t *testing.T, m *model, index string) {
	t.Helper()
	h := selectionHit(t, m, "error", index)
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
}

func TestDiagnosticClickSelectsBeforeOpeningRecovery(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{
		{code: store.CodeGuestPluginSync, scope: "a", detail: "first failure"},
		{code: store.CodeOwnershipConflict, scope: "b", detail: "second failure"},
	}
	clickDiagnostic(t, m, "0")
	if m.focus != "errors" || m.errorIndex != 0 || m.expanded != "" {
		t.Fatalf("first click opened recovery instead of selecting: focus=%s index=%d expanded=%s", m.focus, m.errorIndex, m.expanded)
	}
	clickDiagnostic(t, m, "1")
	if m.focus != "errors" || m.errorIndex != 1 || m.expanded != "" {
		t.Fatalf("clicking another group did not select it: focus=%s index=%d expanded=%s", m.focus, m.errorIndex, m.expanded)
	}
	clickDiagnostic(t, m, "1")
	if m.page != "Sandboxes" || m.focus != "actions" || m.expanded != "b" {
		t.Fatalf("second click did not open recovery: page=%s focus=%s expanded=%s", m.page, m.focus, m.expanded)
	}
}

func TestDiagnosticClickUsesVisibleGroupInNewAndAll(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "New", true: "All"}[all], func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height, m.showAllErrors = 110, 50, all
			// Storage order, grouped order, and visible order deliberately differ.
			m.errors = []uiError{
				{code: store.CodeSSHSetup, scope: "Host", detail: "dismissed host failure", dismissed: true},
				{code: store.CodeGuestPluginSync, scope: "b", pluginID: "one", detail: "first plugin"},
				{code: store.CodeGuestPluginSync, scope: "b", pluginID: "two", detail: "second plugin"},
				{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"},
			}
			clickDiagnostic(t, m, "1")
			if m.focus != "errors" || m.visibleErrors()[m.errorIndex].scope != "b" || m.expanded != "" {
				t.Fatalf("click selected wrong visible group: focus=%s index=%d expanded=%s", m.focus, m.errorIndex, m.expanded)
			}
			m.key(keyMsg(tea.KeyEnter, ""))
			if m.expanded != "b" {
				t.Fatalf("Enter did not recover selected group: expanded=%s", m.expanded)
			}
		})
	}
}

func TestDiagnosticClickTracksGroupAcrossPolling(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: "b", detail: "beta failure"}}
	clickDiagnostic(t, m, "0")
	m.mergeErrors(workerMsg{errors: map[string]store.ErrorRecord{
		"alpha": {Code: store.CodeGuestPluginSync, Scope: "a", Detail: "alpha failure"},
		"beta":  {Code: store.CodeOwnershipConflict, Scope: "b", Detail: "beta updated failure"},
	}})
	clickDiagnostic(t, m, "1")
	if m.expanded != "b" {
		t.Fatalf("poll reorder lost clicked group: expanded=%s", m.expanded)
	}

	m = modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	clickDiagnostic(t, m, "0")
	m.mergeErrors(workerMsg{errors: map[string]store.ErrorRecord{
		"beta": {Code: store.CodeOwnershipConflict, Scope: "b", Detail: "beta failure"},
	}})
	clickDiagnostic(t, m, "0")
	if m.focus != "errors" || m.expanded != "" {
		t.Fatal("replacement group opened recovery on its first click")
	}
}

func TestDiagnosticKeyboardNavigationResetsMouseActivation(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	clickDiagnostic(t, m, "0")
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	clickDiagnostic(t, m, "0")
	if m.focus != "errors" || m.expanded != "" {
		t.Fatal("click after keyboard navigation opened recovery")
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.expanded != "a" {
		t.Fatal("Enter did not open selected recovery immediately")
	}
}

func TestDiagnosticRecoveryResetsMouseActivationForLaterFailure(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	clickDiagnostic(t, m, "0")
	m.mergeErrors(workerMsg{})
	m.mergeErrors(workerMsg{errors: map[string]store.ErrorRecord{
		"alpha": {Code: store.CodeGuestPluginSync, Scope: "a", Detail: "later alpha failure"},
	}})
	clickDiagnostic(t, m, "0")
	if m.focus != "errors" || m.expanded != "" {
		t.Fatal("first click on a later failure opened recovery")
	}
}

func TestDiagnosticDragDoesNotSelectOrOpenRecovery(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	h := selectionHit(t, m, "error", "0")
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	if cmd == nil || m.focus == "errors" || m.expanded != "" {
		t.Fatal("drag failed to copy or activated diagnostic")
	}
	clickDiagnostic(t, m, "0")
	if m.focus != "errors" || m.expanded != "" {
		t.Fatal("first click after drag opened recovery")
	}
}

func TestDiagnosticReleaseKeepsPressedGroupAcrossPolling(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "reordered", true: "recovered"}[recovered], func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = 110, 50
			m.errors = []uiError{{code: store.CodeOwnershipConflict, scope: "b", detail: "beta failure"}}
			h := selectionHit(t, m, "error", "0")
			m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
			errors := map[string]store.ErrorRecord{
				"alpha": {Code: store.CodeGuestPluginSync, Scope: "a", Detail: "alpha failure"},
			}
			if !recovered {
				errors["beta"] = store.ErrorRecord{Code: store.CodeOwnershipConflict, Scope: "b", Detail: "updated beta failure"}
			}
			m.mergeErrors(workerMsg{errors: errors})
			m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
			if recovered {
				if m.focus == "errors" || m.expanded != "" {
					t.Fatal("release selected a replacement for the recovered group")
				}
			} else if m.focus != "errors" || m.visibleErrors()[m.errorIndex].scope != "b" || m.expanded != "" {
				t.Fatal("release lost the pressed group after polling")
			}
		})
	}
}
