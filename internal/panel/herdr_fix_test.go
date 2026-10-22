package panel

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func hostVersionFixture(m *model, v string) {
	m.checks.Checks = []precheck.Check{{Name: "Herdr", Level: precheck.Ready, Detail: v}}
}
func TestGuestVersionArrowsCompareHostWithoutChangingBorderStyle(t *testing.T) {
	for _, tc := range []struct{ guest, host, suffix string }{
		{"0.9.2", "0.9.3", " ↓"}, {"0.10.0", "0.9.3", " ↑"}, {"0.9.3", "0.9.3", ""},
		{"unknown", "0.9.3", ""}, {"0.9.2", "unknown", ""},
	} {
		m := modelFixture(t)
		hostVersionFixture(m, tc.host)
		r := m.rows[0]
		r.Status.GuestServer = &store.GuestServerObservation{Version: tc.guest}
		want := "herdr " + tc.guest + tc.suffix

		got := m.guestServerLabel(r)
		require.Equal(t, want, got, "%s vs %s: %q", tc.guest, tc.host, got)

		for _, width := range []int{36, 60, 110} {
			m.width = width
			lines := m.sandboxDetails(r)
			header := lines[0].text
			require.False(t, width >= 60 && !strings.HasSuffix(ansi.Strip(header), " "+want+" ╮"), "width %d: version not in top-right badge: %q", width, header)
			require.False(t, ansi.StringWidth(header) > width-4, "width %d: %q", width, header)
		}
	}
}
func TestFixHerdrEligibilityAndConfirmation(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.10.0", Current: false}
	require.True(t, m.canFixHerdr(m.rows[0]), "mismatch missing repair")
	m.expanded, m.focus = "a", "actions"
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "fix-herdr" {
			m.button = i
		}
	}

	cmd := m.activate()
	require.Nil(t, cmd)
	require.EqualValues(t, "fix-herdr", m.dialog)
	require.EqualValues(t, 0, m.button)

	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	require.False(t, hint < 0)
	require.Contains(t, view[hint:], "downgrades")
	require.Contains(t, view[hint:], "this sandbox to herdr 0.9.3")
	require.Contains(t, view[hint:], "last known")
	require.Contains(t, view, "alpha (running)")
	hostVersionFixture(m, "0.9.4")
	require.Contains(t, m.fixHerdrExplanationTo(m.rows[0], m.dialogReason), "downgrades this sandbox to herdr 0.9.3", "confirmation target changed under user")
	m.dialogKey("enter")
	require.EqualValues(t, "", m.dialog)
	require.Len(t, m.pending, 0)
	m.rows[0].Status.GuestServer = nil
	m.errors = []uiError{{scope: "a", code: store.CodeHerdrCompatibility}}
	require.True(t, m.canFixHerdr(m.rows[0]), "H0003 without version cannot be fixed")
	require.Contains(t, m.fixHerdrExplanation(m.rows[0]), "unknown", "unknown version invented")
	for _, state := range []string{"stopped", "pending"} {
		row := m.rows[0]
		row.Sandbox.Status = state
		require.False(t, m.canFixHerdr(row), "fix available while %s", state)
	}
	for _, management := range []store.EnabledChoice{store.Disabled, store.Disabled, store.Disabled} {
		row := m.rows[0]
		row.Enabled = management
		require.False(t, m.canFixHerdr(row), "fix available for %s", management)
	}
	row := m.rows[0]
	row.EntryPresent = false
	require.False(t, m.canFixHerdr(row), "fix available for unowned machine")
	hostVersionFixture(m, "unknown")
	require.False(t, m.canFixHerdr(m.rows[0]), "fix has no host target")
}
func TestEveryFocusedDetailsActionExplainsItsEffect(t *testing.T) {
	m := modelFixture(t)
	native := true
	m.rows[0].NativeEnabled = &native
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2", Current: true, CheckedAt: time.Now()}
	m.expanded, m.focus = "a", "actions"
	for i, action := range m.sandboxActions(m.rows[0]) {
		m.button = i
		hint, _ := m.contextHint()
		require.False(t, !strings.HasPrefix(hint, strings.Fields(action.label)[0]) && action.id != "fix-herdr", "%s: %q", action.id, hint)
	}
	m.notice = "Old action completed"
	m.move("home")
	require.Equal(t, "", m.notice, "completed action notice survived navigation")
	m.rows[0].Enabled = store.Disabled
	m.button = 0
	{
		hint, _ := m.contextHint()
		require.True(t, strings.HasPrefix(hint, "Enable this sandbox"), hint)
	}
	m.rows[0].Enabled = store.Disabled
	{
		hint, color := m.contextHint()
		require.True(t, strings.HasPrefix(hint, "Enable this sandbox"))
		require.False(t, color != colorAccent)
	}
}
func TestCompatibilityHintAndDiagnosticUseKnownGuestAndHostVersions(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.0", Current: false}
	m.errors = []uiError{{scope: "a", code: store.CodeHerdrCompatibility, detail: "herdr in this sandbox could not provide machine API forwarding", count: 1}}
	hint, _ := m.contextHint()
	for _, text := range []string{"last known", "0.9.0", "host runs herdr 0.9.3", "Fix herdr", "original installation method"} {
		require.Contains(t, hint, text, "missing %s: %s", text, hint)
	}
	var diagnostic strings.Builder
	for _, l := range m.errorLines() {
		diagnostic.WriteString(ansi.Strip(l.text))
	}
	require.Contains(t, diagnostic.String(), "0.9.0")
	require.Contains(t, diagnostic.String(), "0.9.3")
}
func TestFixHerdrProgressFailureAndRecoveryDoNotInventGuestVersion(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.0"}
	m.run("a", store.CodeHerdrFix, "herdr fix completed", func() error { return nil })

	view := ansi.Strip(m.View().Content)
	require.Contains(t, view, "◐ Fixing herdr for alpha", view)

	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeHerdrFix, err: errors.New("protected installation")})
	require.EqualValues(t, "Fix herdr did not finish.", m.notice)
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].code != store.CodeHerdrFix)
	require.Equal(t, "0.9.0", m.rows[0].Status.GuestServer.Version, "failed fix changed observed version")
	m.errors = append(m.errors, uiError{scope: "a", code: store.CodeHerdrCompatibility, detail: "unverified forwarding"})
	m.Update(operationMsg{target: "a", code: store.CodeHerdrFix, notice: "herdr fix completed"})
	require.True(t, m.rowHasCode(m.rows[0], store.CodeHerdrCompatibility))
	require.False(t, m.rowHasCode(m.rows[0], store.CodeHerdrFix))
	m.dialog, m.dialogTarget, m.dialogReason, m.button = "fix-herdr", "a", "0.9.3", 0
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "", m.dialog)
	require.EqualValues(t, "a", m.expanded)
}

func TestFixHerdrMouseConfirmationStartsActionWithoutOptimisticVersion(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2", Current: true, CheckedAt: time.Now()}
	m.expanded = "a"
	m.hitAction(hit{action: "action", id: "fix-herdr"})
	require.EqualValues(t, "fix-herdr", m.dialog)
	require.EqualValues(t, 0, m.button)
	cmd := m.hitAction(hit{action: "dialog", id: "1"})
	require.NotNil(t, cmd)
	require.True(t, m.pending["a"])
	require.EqualValues(t, "", m.dialog)
	require.Equal(t, "0.9.2", m.rows[0].Status.GuestServer.Version, "confirmation changed observed version")
	require.Contains(t, m.notice, "Fixing herdr", m.notice)
	// Do not execute the external command; the registration package covers it.
}

func TestFixConfirmationRestoresActionByIdentity(t *testing.T) {
	for _, exit := range []string{"cancel", "escape", "approve"} {
		t.Run(exit, func(t *testing.T) {
			m := modelFixture(t)
			hostVersionFixture(m, "0.9.3")
			m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2"}
			m.expanded = "a"
			m.hitAction(hit{action: "action", id: "fix-herdr"})
			// Observation changes remove Re-sync and shift Fix herdr's action index.
			m.rows[0].Status.Connection = "ready"
			var cmd tea.Cmd
			switch exit {
			case "cancel":
				cmd = m.key(keyMsg(tea.KeyEnter, ""))
			case "escape":
				m.key(keyMsg(tea.KeyRight, ""))
				cmd = m.key(keyMsg(tea.KeyEscape, ""))
			case "approve":
				m.key(keyMsg(tea.KeyRight, ""))
				cmd = m.key(keyMsg(tea.KeyEnter, ""))
			}
			actions := m.sandboxActions(m.rows[0])
			require.EqualValues(t, "", m.dialog)
			require.EqualValues(t, "actions", m.focus)
			require.False(t, m.button >= len(actions))
			require.EqualValues(t, "fix-herdr", actions[m.button].id)
			if exit == "approve" {
				require.NotNil(t, cmd)
				require.True(t, m.pending["a"])
				{
					next := m.key(keyMsg(tea.KeyEnter, ""))
					require.Nil(t, next)
					require.EqualValues(t, "", m.dialog)
				}
			} else {
				require.Nil(t, cmd)
				require.Len(t, m.pending, 0)
				{
					next := m.key(keyMsg(tea.KeyEnter, ""))
					require.Nil(t, next)
					require.EqualValues(t, "fix-herdr", m.dialog)
					require.EqualValues(t, 0, m.button)
				}
			}
		})
	}
}

func TestFixConfirmationFallsBackToTableWhenActionDisappears(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2"}
	m.expanded = "a"
	m.hitAction(hit{action: "action", id: "fix-herdr"})
	m.rows[0].Status.GuestServer.Version = "0.9.3"
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, 0, m.button)

	cmd := m.key(keyMsg(tea.KeyEnter, ""))
	require.Nil(t, cmd)
	require.Len(t, m.pending, 0)
}
