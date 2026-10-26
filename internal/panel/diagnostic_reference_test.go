package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestFocusedDiagnosticKeepsReferenceVisibleWithLongPluginDetails(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := modelFixture(t)
	m.width, m.height, m.workerLoaded = 80, 18, true
	require.NoError(t, store.SetGuestPluginFailures("a", []store.PluginFailure{{
		Code: store.CodeGuestPluginSync, PluginID: "first.plugin", Detail: "build failed",
		Output: strings.Repeat("compiler output\n", 200),
	}, {
		Code: store.CodeGuestPluginSync, PluginID: "second.plugin", Detail: "dependency missing",
	}}))
	m.Update(m.pollWorker()())
	ref := m.errors[0].ref
	require.Len(t, ref, 6, "diagnostic reference is missing before rendering")
	m.focusErrors()
	m.Update(m.pollWorker()())
	{
		frame := ansi.Strip(m.View().Content)
		require.Contains(t, frame, ref+" build failed", "focusing active diagnostic scrolled its reference out of view:\n%s", frame)
	}
	for range 3 {
		m.Update(m.pollWorker()())
		{
			frame := ansi.Strip(m.View().Content)
			require.Contains(t, frame, ref+" build failed", "later worker poll hid the active diagnostic reference:\n%s", frame)
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

	header := lines[1].text
	require.Contains(t, header, ink(colorError, "P0002"))
	require.NotContains(t, header, "a1b2c3")

	detail := lines[2]
	require.EqualValues(t, "  a1b2c3 build failed", ansi.Strip(detail.text))
	require.Contains(t, detail.text, ink(colorMuted, "a1b2c3"))
	require.Len(t, detail.hits, 1)
	require.EqualValues(t, "error", detail.hits[0].action)
	require.EqualValues(t, "0", detail.hits[0].id)

	for _, l := range append(lines, m.eventLines()...) {
		require.False(t, ansi.StringWidth(l.text) > m.width-4, "reference exceeded content width: %q", l.text)
	}
	m.errors[0].ref = ""
	{
		got := ansi.Strip(m.errorLines()[1].text)
		require.Equal(t, ansi.Strip(lines[1].text), got, "reference assignment changed metadata position: %q", got)
	}
	{
		got := ansi.Strip(m.errorLines()[2].text)
		require.Equal(t, "  build failed", got, "older diagnostic gained an empty reference spacer: %q", got)
	}
	for _, l := range m.errorLines() {
		require.False(t, strings.Contains(ansi.Strip(l.text), "R0005") && !strings.HasPrefix(ansi.Strip(l.text), "  R0005 · beta"), "older diagnostic has an empty reference prefix: %q", l.text)
	}
}

func TestDiagnosticReferenceSurvivesDismissalPollAndReopen(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	require.NoError(t, store.RecordError(store.CodeRename, "a", "label taken"))
	m.Update(m.pollWorker()())
	m.focusErrors()
	ref := m.visibleErrors()[m.errorIndex].ref
	require.Len(t, ref, 6, "persisted reference not carried into panel: %q", ref)
	m.dismissError()
	persistPanelEffects(m)
	m.Update(m.pollWorker()())
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].ref != ref)
	require.True(t, m.errors[0].dismissed)
	reopened := newModel(nil)
	reopened.Update(reopened.pollWorker()())
	require.Len(t, reopened.errors, 1)
	require.False(t, reopened.errors[0].ref != ref)
	require.True(t, reopened.errors[0].dismissed)
}

func TestAssigningOlderReferenceKeepsDiagnosticSelection(t *testing.T) {
	m := modelFixture(t)
	now := time.Now()
	m.errors = []uiError{{code: store.CodeRename, scope: "a", detail: "label taken", firstSeen: now, seen: now}}
	m.focusErrors()
	m.mergeErrors(workerMsg{observedAt: now.Add(time.Second), errors: map[string]store.ErrorRecord{
		"other": {Ref: "d4e5f6", Code: store.CodePreparation, Scope: "b", Detail: "setup failed", FirstSeen: now, LastSeen: now.Add(-time.Second)},
		"same":  {Ref: "a1b2c3", Code: store.CodeRename, Scope: "a", Detail: "label taken", FirstSeen: now, LastSeen: now},
	}})
	require.EqualValues(t, "errors", m.focus)
	require.EqualValues(t, "a1b2c3", m.visibleErrors()[m.errorIndex].ref)
}
