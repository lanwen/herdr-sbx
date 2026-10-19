package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestFocusedDiagnosticKeepsReferenceVisibleWithLongPluginDetails(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height, m.workerLoaded = 80, 18, true
	if err := store.SetGuestPluginFailures("a", []store.PluginFailure{{
		Code: store.CodeGuestPluginSync, PluginID: "first.plugin", Detail: "build failed",
		Output: strings.Repeat("compiler output\n", 200),
	}, {
		Code: store.CodeGuestPluginSync, PluginID: "second.plugin", Detail: "dependency missing",
	}}); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	ref := m.errors[0].ref
	if len(ref) != 6 {
		t.Fatal("diagnostic reference is missing before rendering")
	}
	m.focusErrors()
	m.Update(m.pollWorker()())
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, ref+" build failed") {
		t.Fatalf("focusing active diagnostic scrolled its reference out of view:\n%s", frame)
	}
	for range 3 {
		m.Update(m.pollWorker()())
		if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, ref+" build failed") {
			t.Fatalf("later worker poll hid the active diagnostic reference:\n%s", frame)
		}
	}
}

func TestDiagnosticReferencesAreMutedAndKeepMouseTargets(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 60
	m.errors = []uiError{{ref: "a1b2c3", code: store.CodeGuestPluginSync, scope: "a", detail: "build failed"}, {code: store.CodeOwnershipConflict, scope: "b", detail: "manual registration"}}
	m.events = []store.EventRecord{{Ref: "a1b2c3", Code: store.CodeGuestPluginSync, Scope: "a", At: time.Now()}}
	lines := m.errorLines()
	if header := lines[1].text; !strings.Contains(header, ink(colorError, "P0002")) || strings.Contains(header, "a1b2c3") {
		t.Fatalf("reference displaced the component code: %q", header)
	}
	if detail := lines[2]; ansi.Strip(detail.text) != "  a1b2c3 build failed" || !strings.Contains(detail.text, ink(colorMuted, "a1b2c3")) || len(detail.hits) != 1 || detail.hits[0].action != "error" || detail.hits[0].id != "0" {
		t.Fatalf("reference lost placement, styling, or recovery target: %+v", detail)
	}
	for _, l := range append(lines, m.eventLines()...) {
		if ansi.StringWidth(l.text) > m.width-4 {
			t.Fatalf("reference exceeded content width: %q", l.text)
		}
	}
	m.errors[0].ref = ""
	if got := ansi.Strip(m.errorLines()[1].text); got != ansi.Strip(lines[1].text) {
		t.Fatalf("reference assignment changed metadata position: %q", got)
	}
	if got := ansi.Strip(m.errorLines()[2].text); got != "  build failed" {
		t.Fatalf("legacy diagnostic gained an empty reference spacer: %q", got)
	}
	for _, l := range m.errorLines() {
		if strings.Contains(ansi.Strip(l.text), "R0005") && !strings.HasPrefix(ansi.Strip(l.text), "  R0005 · beta") {
			t.Fatalf("legacy diagnostic has an empty reference prefix: %q", l.text)
		}
	}
}

func TestDiagnosticReferenceSurvivesDismissalPollAndReopen(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	if err := store.RecordError(store.CodeRename, "a", "label taken"); err != nil {
		t.Fatal(err)
	}
	m.Update(m.pollWorker()())
	m.focusErrors()
	ref := m.visibleErrors()[m.errorIndex].ref
	if len(ref) != 6 {
		t.Fatalf("persisted reference not carried into panel: %q", ref)
	}
	m.dismissError()
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	if len(m.errors) != 1 || m.errors[0].ref != ref || !m.errors[0].dismissed {
		t.Fatalf("poll changed diagnostic reference or dismissal: %+v", m.errors)
	}
	reopened := newModel(nil)
	reopened.Update(reopened.pollWorker()())
	if len(reopened.errors) != 1 || reopened.errors[0].ref != ref || !reopened.errors[0].dismissed {
		t.Fatalf("reopen changed diagnostic reference or dismissal: %+v", reopened.errors)
	}
}

func TestAssigningLegacyReferenceKeepsDiagnosticSelection(t *testing.T) {
	m := modelFixture(t)
	now := time.Now()
	m.errors = []uiError{{code: store.CodeRename, scope: "a", detail: "label taken", firstSeen: now, seen: now}}
	m.focusErrors()
	m.mergeErrors(workerMsg{observedAt: now.Add(time.Second), errors: map[string]store.ErrorRecord{
		"other": {Ref: "d4e5f6", Code: store.CodePreparation, Scope: "b", Detail: "setup failed", FirstSeen: now, LastSeen: now.Add(-time.Second)},
		"same":  {Ref: "a1b2c3", Code: store.CodeRename, Scope: "a", Detail: "label taken", FirstSeen: now, LastSeen: now},
	}})
	if m.focus != "errors" || m.visibleErrors()[m.errorIndex].ref != "a1b2c3" {
		t.Fatalf("reference assignment moved diagnostic selection: %s %d %+v", m.focus, m.errorIndex, m.visibleErrors())
	}
}
