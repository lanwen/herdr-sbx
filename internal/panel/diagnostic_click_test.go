package panel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
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
	selected, preview := m.selected, m.expanded
	m.errors = []uiError{
		{code: store.CodeGuestPluginSync, scope: "a", detail: "first failure"},
		{code: store.CodeOwnershipConflict, scope: "b", detail: "second failure"},
	}
	clickDiagnostic(t, m, "0")
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, 0, m.errorIndex)
	require.False(t, m.selected != selected)
	require.False(t, m.expanded != preview)
	clickDiagnostic(t, m, "1")
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, 1, m.errorIndex)
	require.False(t, m.selected != selected)
	require.False(t, m.expanded != preview)
	clickDiagnostic(t, m, "1")
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
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
			selected, preview := m.selected, m.expanded
			clickDiagnostic(t, m, "1")
			require.EqualValues(t, "errors", m.focus)
			require.EqualValues(t, "b", m.visibleErrors()[m.errorIndex].scope)
			require.False(t, m.selected != selected)
			require.False(t, m.expanded != preview)
			m.key(keyMsg(tea.KeyEnter, ""))
			require.EqualValues(t, "b", m.selected)
			require.EqualValues(t, "b", m.expanded)
			require.EqualValues(t, "table", m.focus)
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
	require.Equal(t, "b", m.expanded, "poll reorder lost clicked group: expanded=%s", m.expanded)

	m = modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	clickDiagnostic(t, m, "0")
	m.mergeErrors(workerMsg{errors: map[string]store.ErrorRecord{
		"beta": {Code: store.CodeOwnershipConflict, Scope: "b", Detail: "beta failure"},
	}})
	clickDiagnostic(t, m, "0")
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
}

func TestDiagnosticKeyboardNavigationResetsMouseActivation(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	clickDiagnostic(t, m, "0")
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	clickDiagnostic(t, m, "0")
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	m.key(keyMsg(tea.KeyEnter, ""))
	require.Equal(t, "a", m.expanded, "Enter did not open selected recovery immediately")
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
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
}

func TestDiagnosticDragDoesNotSelectOrOpenRecovery(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 110, 50
	m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "alpha failure"}}
	h := selectionHit(t, m, "error", "0")
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	require.NotNil(t, cmd)
	require.NotEqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	clickDiagnostic(t, m, "0")
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
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
				require.EqualValues(t, "table", m.focus)
				require.EqualValues(t, "a", m.selected)
				require.EqualValues(t, "a", m.expanded)
			} else {
				require.EqualValues(t, "errors", m.focus)
				require.EqualValues(t, "b", m.visibleErrors()[m.errorIndex].scope)
				require.EqualValues(t, "a", m.selected)
				require.EqualValues(t, "a", m.expanded)
			}
		})
	}
}
