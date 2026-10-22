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
			if scope != "scope" || id != "a" || got != store.Enabled {
				t.Fatalf("Enable dispatch: %s %s %s", scope, id, got)
			}
			return nil
		}
		cmd := m.key(keyMsg(tea.KeySpace, " "))
		if cmd == nil || m.toggleEnabled() != nil || m.rows[0].EntryPresent || m.rows[0].Label != "" {
			t.Fatal("missing dot toggled off, duplicate add, or premature entry")
		}
		m.Update(cmd())
		if calls != 1 {
			t.Fatal(calls)
		}
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
	if cmd := m.toggleEnabled(); cmd != nil {
		t.Fatal("stopped missing entry must remain untouched")
	}
	if m.notice != "" || len(m.errors) != 0 || m.rows[0].EntryPresent {
		t.Fatal("unsupported action reported failure or entry")
	}
	m.focus, m.expanded = "actions", "a"
	if hint, _ := m.contextHint(); strings.Contains(hint, "reports the limitation") {
		t.Fatal(hint)
	}
}

func TestEnabledChoiceChangesBeforeOperationAndSurvivesPoll(t *testing.T) {
	for _, initial := range []store.EnabledChoice{store.Enabled, store.Disabled} {
		for _, outcome := range []string{"success", "failed", "lost acknowledgement"} {
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
				if got != choice {
					t.Fatal("wrong operation")
				}
				if err := store.SetEnabled(scope, id, settled); err != nil {
					return err
				}
				if outcome != "success" {
					return fmt.Errorf("native change %s", outcome)
				}
				return nil
			}
			cmd := m.toggleEnabled()
			if cmd == nil || m.rows[0].Enabled != choice {
				t.Fatal("choice did not update immediately")
			}
			assertPending := func() {
				t.Helper()
				frame := ansi.Strip(m.View().Content)
				if !strings.Contains(frame, progress) {
					t.Fatalf("transition missing: %s", frame)
				}
				line := m.sandboxLines()[3]
				found := false
				for _, h := range line.hits {
					if h.action == "enabled" && h.id == "a" {
						found = strings.TrimSpace(ansi.Cut(ansi.Strip(line.text), h.x, h.x+h.w)) == symbol
					}
				}
				if !found {
					t.Fatalf("choice symbol: %s", ansi.Strip(line.text))
				}
				if m.rows[0].Label != old.Label || m.rows[0].EntryPresent != old.EntryPresent {
					t.Fatal("choice invented entry metadata")
				}
			}
			assertPending()
			m.expanded = "a"
			if !strings.Contains(ansi.Strip(m.sandboxLines()[3].text), progress) {
				t.Fatal("expanded row hid active transition")
			}
			if details := ansi.Strip(strings.Join(lineTexts(m.sandboxDetails(m.rows[0])), "\n")); !strings.Contains(details, progress) {
				t.Fatal("expanded details lost progress")
			}
			m.expanded = ""
			m.Update(observationMsg{generation: m.generation, rows: []registration.SandboxRow{old}})
			if m.rows[0].Enabled != choice {
				t.Fatal("poll replaced pending choice")
			}
			assertPending()
			if m.toggleEnabled() != nil {
				t.Fatal("overlapping operation dispatched")
			}
			generation := m.generation
			m.Update(cmd())
			if m.pending["a"] || m.rows[0].Enabled != settled {
				t.Fatal("completion did not settle to saved actual state")
			}
			m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{old}})
			if m.rows[0].Enabled != settled {
				t.Fatal("old poll replaced settled state")
			}
			observed := m.rows[0]
			actual := settled == store.Enabled
			observed.NativeEnabled = &actual
			observed.Status.Connection = "ready"
			if !actual {
				observed.Status.Connection = "disabled"
			}
			m.Update(observationMsg{generation: m.generation, rows: []registration.SandboxRow{observed}})
			if strings.Contains(ansi.Strip(m.View().Content), progress) {
				t.Fatal("completed transition remained")
			}
			if outcome != "success" && len(m.errors) == 0 {
				t.Fatal("operation failure hidden")
			}
			if m.toggleEnabled() == nil {
				t.Fatal("settled operation not actionable")
			}
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
	if cmd == nil || m.rows[0].Enabled != store.Enabled || m.rows[0].EntryPresent || m.rows[0].Label != "" {
		t.Fatal("optimistic choice invented entry")
	}
	line := ansi.Strip(m.sandboxLines()[3].text)
	if !strings.Contains(line, "✓") || !strings.Contains(line, "Enabling…") {
		t.Fatal(line)
	}
	m.Update(cmd())
	if m.rows[0].Enabled != store.Enabled || m.rows[0].Label != "" {
		t.Fatal("settled saved choice lost or label invented")
	}
}
func TestDisableRemoveEnableEligibilityWhileManagementOff(t *testing.T) {
	m := modelFixture(t)
	m.workerEnabled = false
	m.refreshing = true
	m.rows[0].Enabled = store.Disabled
	native := true
	m.rows[0].NativeEnabled = &native
	if canRemoveEntry(m.rows[0]) {
		t.Fatal("native connection still enabled")
	}
	m.rows[0].NativeEnabled = nil
	if canRemoveEntry(m.rows[0]) {
		t.Fatal("unknown native state permits removal")
	}
	native = false
	m.rows[0].NativeEnabled = &native
	if !canRemoveEntry(m.rows[0]) {
		t.Fatal("confirmed disabled entry cannot be removed")
	}
	m.removeEntryFn = func(context.Context, string, string) error { return fmt.Errorf("removal failed") }
	m.Update(m.removeEntry()())
	if m.rows[0].Label != "API desk" || !m.rows[0].EntryPresent || !canRemoveEntry(m.rows[0]) || m.workerEnabled {
		t.Fatal("failed removal lost entry, retry, or OFF")
	}
	m.beginRename()
	if m.input == nil {
		t.Fatal("rename blocked by Management OFF/Disabled")
	}
	m.input = nil
	m.focus, m.expanded, m.button = "actions", "a", 0
	m.rows[0].Sandbox.Status = "stopped"
	if hint, _ := m.contextHint(); !strings.Contains(hint, "Herdr may start it when connecting.") {
		t.Fatal(hint)
	}
	m.focus = "table"
	m.rows[0].Enabled = store.Enabled
	for _, state := range []string{"disabled in herdr", "enable not confirmed"} {
		m.rows[0].Status.Connection = state
		m.rows[0].Status.Detail = "Enabled is saved, but the native herdr connection is disabled. Retry Enable to enable its connection."
		if state == "enable not confirmed" {
			m.rows[0].NativeEnabled = nil
		}
		if hint, _ := m.contextHint(); hint != m.rows[0].Status.Detail {
			t.Fatal("stopped hint hid enable failure:", hint)
		}

	}
	native = true
	m.rows[0].NativeEnabled = &native
	m.rows[0].Status.Connection = "waiting for start"
	if hint, _ := m.contextHint(); !strings.Contains(hint, "Setup waits until it starts") {
		t.Fatal(hint)
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
		if !strings.Contains(frame, colorMuted+"Cancelling") {
			t.Fatal("cancellation lost exact gray state")
		}
		lines := strings.Split(ansi.Strip(frame), "\n")
		if len(lines) != 10 || !strings.Contains(lines[9], "Management OFF") {
			t.Fatalf("footer/height at %d: %s", width, frame)
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > width {
				t.Fatalf("overflow at %d: %q", width, l)
			}
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
			if !strings.Contains(ansi.Cut(strings.Split(frame, "\n")[h.y], h.x, h.x+h.w), "Remove from herdr") {
				t.Fatal("full removal label clipped")
			}
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
	if len(m.rows) != 2 || m.rows[0].Label != "External label" || !m.rows[0].Status.Stale || !m.rows[1].Status.Stale {
		t.Fatal("partial failure erased entries or labels")
	}
	hint, _ := m.contextHint()
	if !strings.Contains(hint, "Last observation") || !strings.Contains(hint, "last known") {
		t.Fatal(hint)
	}
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
			if strings.HasPrefix(action.label, "Retry") {
				t.Fatal("settled choice still exposes mismatch retry", actions)
			}
		}
		if actions[0].id != actionID || strings.HasPrefix(actions[0].label, "Retry") {
			t.Fatal(actions)
		}
		m.setEnabledFn = func(_ context.Context, scope, id string, choice store.EnabledChoice) error {
			if choice != wanted {
				t.Fatal("ordinary action chose wrong state")
			}
			return store.SetEnabled(scope, id, choice)
		}
		cmd := m.activate()
		if cmd == nil || m.activate() != nil {
			t.Fatal("action absent or duplicate accepted")
		}
		m.Update(cmd())
		if m.rows[0].Enabled != wanted {
			t.Fatal("action did not settle")
		}
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
	if hint < 0 || !strings.Contains(frame[hint:], colorMuted+"Cancelling") || strings.Contains(frame[hint:], "◐ Cancelling") {
		t.Fatal("Hint does not show exact gray cancellation")
	}
}

func TestEnabledResultUsesSavedChoiceWhenRefreshFails(t *testing.T) {
	for _, quietNoop := range []bool{false, true} {
		m := modelFixture(t)
		m.refreshing = true
		original := m.rows[0]
		requested, settled := store.Disabled, store.Enabled
		if quietNoop {
			original.EntryPresent, original.Label, original.Enabled = false, "", store.Disabled
			m.rows[0] = original
			requested, settled = store.Enabled, store.Disabled
		}
		if err := store.SetEnabled("scope", "a", settled); err != nil {
			t.Fatal(err)
		}
		m.setEnabledFn = func(context.Context, string, string, store.EnabledChoice) error {
			if quietNoop {
				return store.SetEnabled("scope", "a", store.Disabled)
			}
			return fmt.Errorf("saving enabled choice failed")
		}
		cmd := m.toggleEnabled()
		if cmd == nil || m.rows[0].Enabled != requested {
			t.Fatal("request not immediate")
		}
		generation := m.generation
		m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{original}})
		if m.rows[0].Enabled != requested {
			t.Fatal("poll reset pending request")
		}
		m.Update(cmd())
		if m.rows[0].Enabled != settled {
			t.Fatal("completion ignored unchanged saved choice")
		}
		stale := original
		stale.Enabled = requested
		m.Update(observationMsg{generation: generation, rows: []registration.SandboxRow{stale}})
		m.Update(observationMsg{generation: m.generation, err: fmt.Errorf("catalog unavailable")})
		if m.rows[0].Enabled != settled || m.rows[0].Label != original.Label || m.rows[0].EntryPresent != original.EntryPresent {
			t.Fatal("failed refresh restored optimistic state")
		}
		if quietNoop {
			for _, e := range m.errors {
				if e.code == store.CodeManagement {
					t.Fatal("quiet no-op complained")
				}
			}
		}
	}
}
