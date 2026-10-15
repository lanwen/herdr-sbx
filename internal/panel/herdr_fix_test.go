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
		if got := m.guestServerLabel(r); got != want {
			t.Fatalf("%s vs %s: %q", tc.guest, tc.host, got)
		}
		for _, width := range []int{36, 60, 110} {
			m.width = width
			lines := m.sandboxDetails(r)
			footer := lines[len(lines)-1].text
			if !strings.HasSuffix(ansi.Strip(footer), " "+want+" ─╯") || ansi.StringWidth(footer) > width-4 {
				t.Fatalf("width %d: %q", width, footer)
			}
			if colorsEnabled() && footer != ink(colorSeparator, ansi.Strip(footer)) {
				t.Fatal("version arrow changed border style")
			}
		}
	}
}
func TestFixHerdrEligibilityAndConfirmation(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.10.0", Current: false}
	if !m.canFixHerdr(m.rows[0]) {
		t.Fatal("mismatch missing repair")
	}
	m.expanded, m.focus = "a", "actions"
	for i, a := range m.sandboxActions(m.rows[0]) {
		if a.id == "fix-herdr" {
			m.button = i
		}
	}
	if cmd := m.activate(); cmd != nil || m.dialog != "fix-herdr" || m.button != 0 {
		t.Fatal("fix did not default confirmation to Cancel")
	}
	view := ansi.Strip(m.View().Content)
	hint := strings.Index(view, "╭─ Hint")
	if hint < 0 || (!strings.Contains(view[hint:], "downgrades") || !strings.Contains(view[hint:], "this sandbox to herdr 0.9.3")) || !strings.Contains(view[hint:], "last known") || !strings.Contains(view, "╭─ Details") {
		t.Fatalf("missing contextual downgrade confirmation: %s", view)
	}
	hostVersionFixture(m, "0.9.4")
	if !strings.Contains(m.fixHerdrExplanationTo(m.rows[0], m.dialogReason), "downgrades this sandbox to herdr 0.9.3") {
		t.Fatal("confirmation target changed under user")
	}
	m.dialogKey("enter")
	if m.dialog != "" || len(m.pending) != 0 {
		t.Fatal("Cancel started work")
	}
	m.rows[0].Status.GuestServer = nil
	m.errors = []uiError{{scope: "a", code: store.CodeHerdrCompatibility}}
	if !m.canFixHerdr(m.rows[0]) {
		t.Fatal("H0003 without version cannot be fixed")
	}
	if !strings.Contains(m.fixHerdrExplanation(m.rows[0]), "unknown") {
		t.Fatal("unknown version invented")
	}
	for _, state := range []string{"stopped", "pending"} {
		row := m.rows[0]
		row.Sandbox.Status = state
		if m.canFixHerdr(row) {
			t.Fatalf("fix available while %s", state)
		}
	}
	for _, management := range []store.ManagementChoice{store.Unmanaged, store.Unwatched, store.Disabled} {
		row := m.rows[0]
		row.Management = management
		if m.canFixHerdr(row) {
			t.Fatalf("fix available for %s", management)
		}
	}
	row := m.rows[0]
	row.Owned = false
	if m.canFixHerdr(row) {
		t.Fatal("fix available for unowned machine")
	}
	hostVersionFixture(m, "unknown")
	if m.canFixHerdr(m.rows[0]) {
		t.Fatal("fix has no host target")
	}
}
func TestEveryFocusedDetailsActionExplainsItsEffect(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2", Current: true, CheckedAt: time.Now()}
	m.expanded, m.focus = "a", "actions"
	for i, action := range m.sandboxActions(m.rows[0]) {
		m.button = i
		hint, _ := m.contextHint()
		if !strings.HasPrefix(hint, strings.Fields(action.label)[0]) && action.id != "fix-herdr" {
			t.Fatalf("%s: %q", action.id, hint)
		}
	}
	m.notice = "Old action completed"
	m.move("home")
	if m.notice != "" {
		t.Fatal("completed action notice survived navigation")
	}
	m.rows[0].Management = store.Unwatched
	m.button = 0
	if hint, _ := m.contextHint(); !strings.HasPrefix(hint, "Watch lets") {
		t.Fatal(hint)
	}
	m.rows[0].Management = store.Unmanaged
	if hint, color := m.contextHint(); !strings.HasPrefix(hint, "Adopt lets") || color != colorAccent {
		t.Fatal(hint, color)
	}
}
func TestCompatibilityHintAndDiagnosticUseKnownGuestAndHostVersions(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.0", Current: false}
	m.errors = []uiError{{scope: "a", code: store.CodeHerdrCompatibility, detail: "herdr in this sandbox could not provide machine API forwarding", count: 1}}
	hint, _ := m.contextHint()
	for _, text := range []string{"last known", "0.9.0", "host runs herdr 0.9.3", "Fix herdr", "original installation method"} {
		if !strings.Contains(hint, text) {
			t.Fatalf("missing %s: %s", text, hint)
		}
	}
	var diagnostic strings.Builder
	for _, l := range m.errorLines() {
		diagnostic.WriteString(ansi.Strip(l.text))
	}
	if !strings.Contains(diagnostic.String(), "0.9.0") || !strings.Contains(diagnostic.String(), "0.9.3") {
		t.Fatal(diagnostic.String())
	}
}
func TestFixHerdrProgressFailureAndRecoveryDoNotInventGuestVersion(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.0"}
	m.run("a", store.CodeHerdrFix, "herdr fix completed", func() error { return nil })
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "◐ Fixing herdr for alpha") {
		t.Fatal(view)
	}
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodeHerdrFix, err: errors.New("protected installation")})
	if m.notice != "Fix herdr did not finish." || len(m.errors) != 1 || m.errors[0].code != store.CodeHerdrFix {
		t.Fatal("failed fix lost diagnostic or result")
	}
	if m.rows[0].Status.GuestServer.Version != "0.9.0" {
		t.Fatal("failed fix changed observed version")
	}
	m.errors = append(m.errors, uiError{scope: "a", code: store.CodeHerdrCompatibility, detail: "unverified forwarding"})
	m.Update(operationMsg{target: "a", code: store.CodeHerdrFix, notice: "herdr fix completed"})
	if !m.rowHasCode(m.rows[0], store.CodeHerdrCompatibility) || m.rowHasCode(m.rows[0], store.CodeHerdrFix) {
		t.Fatal("fix completion cleared original incompatibility")
	}
	m.dialog, m.dialogTarget, m.dialogReason, m.button = "fix-herdr", "a", "0.9.3", 0
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.dialog != "" || m.expanded != "a" {
		t.Fatal("Esc did not cancel within details")
	}
}

func TestFixHerdrMouseConfirmationStartsActionWithoutOptimisticVersion(t *testing.T) {
	m := modelFixture(t)
	hostVersionFixture(m, "0.9.3")
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.2", Current: true, CheckedAt: time.Now()}
	m.expanded = "a"
	m.hitAction(hit{action: "action", id: "fix-herdr"})
	if m.dialog != "fix-herdr" || m.button != 0 {
		t.Fatal("mouse did not open safe confirmation")
	}
	cmd := m.hitAction(hit{action: "dialog", id: "1"})
	if cmd == nil || !m.pending["a"] || m.dialog != "" {
		t.Fatal("confirm did not schedule repair")
	}
	if m.rows[0].Status.GuestServer.Version != "0.9.2" {
		t.Fatal("confirmation changed observed version")
	}
	if !strings.Contains(m.notice, "Fixing herdr") {
		t.Fatal(m.notice)
	}
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
			if m.dialog != "" || m.focus != "actions" || m.button >= len(actions) || actions[m.button].id != "fix-herdr" {
				t.Fatalf("%s restored dangerous focus: %s %d", exit, m.focus, m.button)
			}
			if exit == "approve" {
				if cmd == nil || !m.pending["a"] {
					t.Fatal("approval did not schedule fix")
				}
				if next := m.key(keyMsg(tea.KeyEnter, "")); next != nil || m.dialog != "" {
					t.Fatal("Enter scheduled another action while fix pending")
				}
			} else {
				if cmd != nil || len(m.pending) != 0 {
					t.Fatal("cancel scheduled work")
				}
				if next := m.key(keyMsg(tea.KeyEnter, "")); next != nil || m.dialog != "fix-herdr" || m.button != 0 {
					t.Fatal("Enter after cancellation did not safely reopen Fix confirmation")
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
	if m.focus != "table" || m.button != 0 {
		t.Fatal("missing Fix fell onto another action")
	}
	if cmd := m.key(keyMsg(tea.KeyEnter, "")); cmd != nil || len(m.pending) != 0 {
		t.Fatal("Enter after missing Fix scheduled a destructive action")
	}
}
