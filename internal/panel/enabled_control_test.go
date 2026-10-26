package panel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMissingDotEnablesAndSuppressesDuplicateAdds(t *testing.T) {
	for _, choice := range []store.EnabledChoice{store.Enabled, store.Disabled} {
		m := modelFixture(t)
		m.refreshing = true
		m.rows[0].EntryPresent = false
		m.rows[0].Label = ""
		m.rows[0].Enabled = choice
		calls := 0
		m.setEnabledFn = func(_ context.Context, scope, id string, got store.EnabledChoice) error {
			calls++
			require.EqualValues(t, "scope", scope)
			require.EqualValues(t, "a", id)
			require.False(t, got != store.Enabled)
			return nil
		}
		cmd := m.key(keyMsg(tea.KeySpace, " "))
		require.NotNil(t, cmd)
		require.Nil(t, m.toggleEnabled())
		require.False(t, m.rows[0].EntryPresent)
		require.EqualValues(t, "", m.rows[0].Label)
		m.Update(cmd())
		require.Equal(t, 1, calls, calls)
	}
}
func TestStoppedMissingEntryEnableIsQuiet(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].EntryPresent, m.rows[0].Label = false, ""
	m.rows[0].Sandbox.Status = "stopped"
	m.setEnabledFn = func(context.Context, string, string, store.EnabledChoice) error {
		t.Fatal("unsupported action dispatched")
		return nil
	}

	cmd := m.toggleEnabled()
	require.Nil(t, cmd, "stopped missing entry must remain untouched")

	require.EqualValues(t, "", m.notice)
	require.Len(t, m.errors, 0)
	require.False(t, m.rows[0].EntryPresent)
	m.focus, m.expanded = "actions", "a"

	hint, _ := m.contextHint()
	require.NotContains(t, hint, "reports the limitation", hint)
}

func TestEnabledChoiceChangesBeforeOperationAndSurvivesPoll(t *testing.T) {
	for _, initial := range []store.EnabledChoice{store.Enabled, store.Disabled} {
		for _, outcome := range []string{"success", "failed", "lost acknowledgement"} {
			testPendingEnabledChoice(t, initial, outcome)
		}
	}
}

func TestMissingRunningEntryShowsImmediateEnableUntilActualResult(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	m.rows[0].Enabled, m.rows[0].EntryPresent, m.rows[0].Label = store.Disabled, false, ""
	m.setEnabledFn = func(_ context.Context, scope, id string, choice store.EnabledChoice) error {
		return store.SetEnabled(scope, id, choice)
	}
	cmd := m.toggleEnabled()
	require.NotNil(t, cmd)
	require.False(t, m.rows[0].Enabled != store.Enabled)
	require.False(t, m.rows[0].EntryPresent)
	require.EqualValues(t, "", m.rows[0].Label)
	line := ansi.Strip(m.sandboxLines()[3].text)
	require.Contains(t, line, "✓")
	require.Contains(t, line, "Enabling…")
	m.Update(cmd())
	require.False(t, m.rows[0].Enabled != store.Enabled)
	require.EqualValues(t, "", m.rows[0].Label)
}
func TestDisableRemoveEnableEligibilityWhileManagementOff(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = false
	m.refreshing = true
	m.rows[0].Enabled = store.Disabled
	native := true
	m.rows[0].NativeEnabled = &native
	require.False(t, canRemoveEntry(m.rows[0]), "native connection still enabled")
	m.rows[0].NativeEnabled = nil
	require.False(t, canRemoveEntry(m.rows[0]), "unknown native state permits removal")
	native = false
	m.rows[0].NativeEnabled = &native
	require.True(t, canRemoveEntry(m.rows[0]), "confirmed disabled entry cannot be removed")
	m.removeEntryFn = func(context.Context, string, string) error { return fmt.Errorf("removal failed") }
	m.Update(m.removeEntry()())
	require.EqualValues(t, "API desk", m.rows[0].Label)
	require.True(t, m.rows[0].EntryPresent)
	require.True(t, canRemoveEntry(m.rows[0]))
	require.False(t, m.workerEnabled)
	m.beginRename()
	require.NotNil(t, m.input, "rename blocked by Management OFF/Disabled")
	m.input = nil
	m.focus, m.expanded, m.button = "actions", "a", 0
	m.rows[0].Sandbox.Status = "stopped"
	{
		hint, _ := m.contextHint()
		require.Contains(t, hint, "Herdr may start it when connecting.", hint)
	}
	m.focus = "table"
	m.rows[0].Enabled = store.Enabled
	for _, state := range []string{"disabled in herdr", "enable not confirmed"} {
		m.rows[0].Status.Connection = state
		m.rows[0].Status.Detail = "Enabled is saved, but the native herdr connection is disabled. Retry Enable to enable its connection."
		if state == "enable not confirmed" {
			m.rows[0].NativeEnabled = nil
		}
		{
			hint, _ := m.contextHint()
			require.Equal(t, m.rows[0].Status.Detail, hint, "%v %v", "stopped hint hid enable failure:", hint)
		}
	}
	native = true
	m.rows[0].NativeEnabled = &native
	m.rows[0].Status.Connection = "waiting for start"
	{
		hint, _ := m.contextHint()
		require.Contains(t, hint, "Setup waits until it starts", hint)
	}
}
func TestCancellationFooterFitsPhoneAndStaysGray(t *testing.T) {
	for _, width := range []int{30, 32, 40, 110} {
		m := modelFixture(t)
		unsetNoColor(t)
		m.width = width
		m.height = 10
		m.stopRequestedAt = time.Now()
		m.worker.State = "running"
		frame := m.View().Content
		require.Contains(t, frame, colorMuted+"Cancelling", "cancellation lost exact gray state")
		lines := strings.Split(ansi.Strip(frame), "\n")
		require.Len(t, lines, 10)
		require.Contains(t, lines[9], "Management OFF")
		for _, l := range lines {
			require.False(t, ansi.StringWidth(l) > width, "overflow at %d: %q", width, l)
		}
	}
}

func TestPhoneRemoveActionKeepsFullLabelAndMouseTarget(t *testing.T) {
	m := modelFixture(t)
	m.width, m.height = 30, 10
	m.expanded = "a"
	m.focus = "actions"
	m.rows[0].Enabled = store.Disabled
	native := false
	m.rows[0].NativeEnabled = &native
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "remove" {
			m.button = i
		}
	}
	m.reveal = true
	frame := ansi.Strip(m.View().Content)
	for _, h := range m.hits {
		if h.action == "action" && h.id == "remove" {
			require.Contains(t, ansi.Cut(strings.Split(frame, "\n")[h.y], h.x, h.x+h.w), "Remove from herdr", "full removal label clipped")
			return
		}
	}
	t.Fatal("remove button unreachable")
}

func TestCatalogFailureUsesCachedRowsAndRetainsPriorEntries(t *testing.T) {
	m := modelFixture(t)
	m.refreshing = true
	cached := m.rows[0]
	cached.Label = "External label"
	cached.Status.Detail = "Catalog unavailable; label is last known."
	cached.Status.ObservedAt = time.Now().Add(-time.Minute)
	m.Update(observationMsg{rows: []registration.SandboxRow{cached}, err: fmt.Errorf("catalog unavailable")})
	require.Len(t, m.rows, 2)
	require.EqualValues(t, "External label", m.rows[0].Label)
	require.True(t, m.rows[0].Status.Stale)
	require.True(t, m.rows[1].Status.Stale)
	hint, _ := m.contextHint()
	require.Contains(t, hint, "Last observation")
	require.Contains(t, hint, "last known")
}

func TestSettledStateOffersOrdinaryEnableOrDisable(t *testing.T) {
	for _, initial := range []store.EnabledChoice{store.Enabled, store.Disabled} {
		m := modelFixture(t)
		m.refreshing = true
		m.rows[0].Enabled = initial
		native := initial == store.Enabled
		m.rows[0].NativeEnabled = nil
		wanted, actionID := store.Enabled, "enable"
		if native {
			wanted, actionID = store.Disabled, "disable"
		}
		m.focus = "actions"
		m.button = 0
		actions := m.sandboxActions(m.rows[0])
		for _, action := range actions {
			require.False(t, strings.HasPrefix(action.label, "Retry"), "%v %v", "settled choice still exposes mismatch retry", actions)
		}
		require.False(t, actions[0].id != actionID)
		require.False(t, strings.HasPrefix(actions[0].label, "Retry"))
		m.setEnabledFn = func(_ context.Context, scope, id string, choice store.EnabledChoice) error {
			require.Equal(t, wanted, choice, "ordinary action chose wrong state")
			return store.SetEnabled(scope, id, choice)
		}
		cmd := m.activate()
		require.NotNil(t, cmd)
		require.Nil(t, m.activate())
		m.Update(cmd())
		require.Equal(t, wanted, m.rows[0].Enabled, "action did not settle")
	}
}

func TestCancellationHintUsesGrayWithoutSpinner(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	m.workerEnabled = true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return nil }
	m.setWorkerEnabled(false)
	frame := m.View().Content
	hint := strings.Index(frame, "Hint")
	require.False(t, hint < 0)
	require.Contains(t, frame[hint:], colorMuted+"Cancelling")
	require.NotContains(t, frame[hint:], "◐ Cancelling")
}

func TestEnabledResultUsesSavedChoiceWhenRefreshFails(t *testing.T) {
	for _, quietNoop := range []bool{false, true} {
		testEnabledRefreshFailure(t, quietNoop)
	}
}

func testPendingEnabledChoice(t *testing.T, initial store.EnabledChoice, outcome string) {
	t.Helper()
	m := modelFixture(t)
	m.refreshing = true
	m.rows[0].Enabled = initial
	native := initial == store.Enabled
	m.rows[0].NativeEnabled = &native
	old := m.rows[0]
	choice, progress, symbol := store.Enabled, "Enabling…", "✓"
	if initial == store.Enabled {
		choice, progress, symbol = store.Disabled, "Disabling…", "·"
	}
	settled := choice
	if outcome == "failed" {
		settled = initial
	}
	m.setEnabledFn = func(_ context.Context, scope, id string, got store.EnabledChoice) error {
		require.Equal(t, choice, got, "wrong operation")
		if err := store.SetEnabled(scope, id, settled); err != nil {
			return err
		}
		if outcome != "success" {
			return fmt.Errorf("native change %s", outcome)
		}
		return nil
	}
	cmd := m.toggleEnabled()
	require.NotNil(t, cmd)
	require.False(t, m.rows[0].Enabled != choice)
	assertPending := func() { assertPendingEnabledChoice(t, m, old, progress, symbol) }

	assertPending()
	m.expanded = "a"
	require.Contains(t, ansi.Strip(m.sandboxLines()[3].text), progress, "expanded row hid active transition")

	details := ansi.Strip(strings.Join(lineTexts(m.sandboxDetails(m.rows[0])), "\n"))
	require.Contains(t, details, progress, "expanded details lost progress")

	m.expanded = ""
	m.Update(observationMsg{generation: m.generation, rows: []registration.SandboxRow{old}})
	require.Equal(t, choice, m.rows[0].Enabled, "poll replaced pending choice")
	assertPending()
	require.Nil(t, m.toggleEnabled(), "overlapping operation dispatched")
	generation := m.generation
	m.Update(cmd())
	require.False(t, m.pending["a"])
	require.False(t, m.rows[0].Enabled != settled)
	m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{old}})
	require.Equal(t, settled, m.rows[0].Enabled, "old poll replaced settled state")
	observed := m.rows[0]
	actual := settled == store.Enabled
	observed.NativeEnabled = &actual
	observed.Status.Connection = "ready"
	if !actual {
		observed.Status.Connection = "disabled"
	}
	m.Update(observationMsg{generation: m.generation, rows: []registration.SandboxRow{observed}})
	require.NotContains(t, ansi.Strip(m.View().Content), progress, "completed transition remained")
	require.False(t, outcome != "success" && len(m.errors) == 0, "operation failure hidden")
	require.NotNil(t, m.toggleEnabled(), "settled operation not actionable")

}

func testEnabledRefreshFailure(t *testing.T, quietNoop bool) {
	t.Helper()
	m := modelFixture(t)
	m.refreshing = true
	original := m.rows[0]
	requested, settled := store.Disabled, store.Enabled
	if quietNoop {
		original.EntryPresent, original.Label, original.Enabled = false, "", store.Disabled
		m.rows[0] = original
		requested, settled = store.Enabled, store.Disabled
	}
	require.NoError(t, store.SetEnabled("scope", "a", settled))
	m.setEnabledFn = func(context.Context, string, string, store.EnabledChoice) error {
		if quietNoop {
			return store.SetEnabled("scope", "a", store.Disabled)
		}
		return fmt.Errorf("saving enabled choice failed")
	}
	cmd := m.toggleEnabled()
	require.NotNil(t, cmd)
	require.False(t, m.rows[0].Enabled != requested)
	generation := m.generation
	m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{original}})
	require.Equal(t, requested, m.rows[0].Enabled, "poll reset pending request")
	m.Update(cmd())
	require.Equal(t, settled, m.rows[0].Enabled, "completion ignored unchanged saved choice")
	stale := original
	stale.Enabled = requested
	m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{stale}})
	m.Update(observationMsg{generation: m.generation, err: fmt.Errorf("catalog unavailable")})
	require.False(t, m.rows[0].Enabled != settled)
	require.False(t, m.rows[0].Label != original.Label)
	require.False(t, m.rows[0].EntryPresent != original.EntryPresent)
	if quietNoop {
		for _, e := range m.errors {
			require.NotEqual(t, store.CodeManagement, e.code, "quiet no-op complained")
		}
	}

}

func assertPendingEnabledChoice(t *testing.T, m *model, old registration.SandboxRow, progress, symbol string) {

	t.Helper()
	frame := ansi.Strip(m.View().Content)
	require.Contains(t, frame, progress, "transition missing: %s", frame)
	line := m.sandboxLines()[3]
	found := false
	for _, h := range line.hits {
		if h.action == "enabled" && h.id == "a" {
			found = strings.TrimSpace(ansi.Cut(ansi.Strip(line.text), h.x, h.x+h.w)) == symbol
		}
	}
	require.True(t, found, "choice symbol: %s", ansi.Strip(line.text))
	require.False(t, m.rows[0].Label != old.Label)
	require.False(t, m.rows[0].EntryPresent != old.EntryPresent)
}
