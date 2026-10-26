package panel

import (
	"github.com/lanwen/herdr-sbx/internal/registration"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func selectionHit(t *testing.T, m *model, action, id string) hit {
	t.Helper()
	m.View()
	for _, h := range m.hits {
		if h.action == action && h.id == id {
			return h
		}
	}
	t.Fatalf("missing %s/%s hit", action, id)
	return hit{}
}

func assertClipboard(t *testing.T, command tea.Cmd, want string) {
	t.Helper()
	require.NotNil(t, command, "clipboard command missing")

	got, expected := command(), tea.SetClipboard(want)()
	require.False(t, got != expected, "clipboard command = %#v, want %#v", got, expected)
}

func TestMouseClickActivatesOriginalTargetOnceOnRelease(t *testing.T) {
	unsetNoColor(t)
	m := styledFixture(t)
	m.focus = "actions"
	h := selectionHit(t, m, "select", "b")
	require.Greater(t, h.x, 4, "row target must include the outline inset")
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	require.Contains(t, ansi.Cut(rows[h.y], h.x, h.x+h.w), "beta", "row selection hit misses sandbox name")
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	// Even if a later observation provides different hit targets, release must
	// activate the one captured from the displayed frame at press time.
	m.hits = []hit{{x: h.x, y: h.y, w: h.w, action: "select", id: "a"}}
	release := tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft}
	m.Update(release)
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.Equal(t, "table", m.focus)
	m.Update(release)
	require.Equal(t, "b", m.expanded, "duplicate release activated the row twice")
	h = selectionHit(t, m, "select", "b")
	clickAt(m, h.x, h.y)
	require.Equal(t, "b", m.selected)
	require.Equal(t, "b", m.expanded, "same-card click removed preview")
	require.Equal(t, "table", m.focus)
}

func TestDragCopiesWithoutActivatingAndLatchesAfterOneCell(t *testing.T) {
	for _, returnToAnchor := range []bool{false, true} {
		m := modelFixture(t)
		m.renderedFrame = "alpha"
		m.hits = []hit{{x: 0, y: 0, w: 5, action: "select", id: "b"}}
		m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: 1, Y: 0, Button: tea.MouseLeft})
		end, want := 3, "alph"
		if returnToAnchor {
			end, want = 0, "a"
			m.Update(tea.MouseMotionMsg{X: 0, Y: 0, Button: tea.MouseLeft})
		}
		_, command := m.Update(tea.MouseReleaseMsg{X: end, Y: 0, Button: tea.MouseLeft})
		assertClipboard(t, command, want)
		require.EqualValues(t, "a", m.selected)
		require.EqualValues(t, "a", m.expanded)
		require.NotNil(t, m.textSelection)
		require.False(t, m.textSelection.pressed)
		view := m.View()
		require.False(t, view.MouseMode != tea.MouseModeCellMotion)
		require.Contains(t, view.Content, "\x1b[7m")
		require.EqualValues(t, "alpha", ansi.Strip(view.Content))
		{
			_, command := m.Update(tea.MouseReleaseMsg{X: end, Y: 0, Button: tea.MouseLeft})
			require.Nil(t, command, "duplicate release copied again")
		}
	}
}

func TestTextSelectionCopiesPlainGraphemesAcrossLines(t *testing.T) {
	frame := "\x1b[31mA界e\u0301👩‍💻Z\x1b[0m  \nsecond line  \nlast"
	for _, test := range []struct {
		name        string
		anchor, end selectionPoint
		want        string
	}{
		{"wide second cell", selectionPoint{2, 0}, selectionPoint{2, 0}, "界"},
		{"wide reverse", selectionPoint{2, 0}, selectionPoint{1, 0}, "界"},
		{"combining and emoji", selectionPoint{3, 0}, selectionPoint{4, 0}, "e\u0301👩‍💻"},
		{"emoji second cell", selectionPoint{5, 0}, selectionPoint{6, 0}, "👩‍💻Z"},
		{"multiline", selectionPoint{1, 0}, selectionPoint{2, 2}, "界e\u0301👩‍💻Z\nsecond line\nlas"},
		{"multiline reverse", selectionPoint{2, 2}, selectionPoint{1, 0}, "界e\u0301👩‍💻Z\nsecond line\nlas"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &textSelection{frame: frame, anchor: test.anchor, end: test.end, dragged: true}

			got := s.text()
			require.Equal(t, test.want, got, "copied %q, want %q", got, test.want)

			require.False(t, ansi.Strip(s.view()) != ansi.Strip(frame))
			require.Contains(t, s.view(), "\x1b[7m")
			original, highlighted := strings.Split(frame, "\n"), strings.Split(s.view(), "\n")
			for y := range original {
				require.Equal(t, ansi.StringWidth(highlighted[y]), ansi.StringWidth(original[y]), "selection highlight changed cell geometry")
			}
		})
	}
}

func TestDragFreezesDisplayedFrameWhileObservationsContinue(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	h := selectionHit(t, m, "select", "a")
	frame := m.View().Content
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}})
	rows := append([]registration.SandboxRow(nil), m.rows...)
	rows[0].Sandbox.Name = "updated alpha"
	m.Update(observationMsg{generation: m.generation, rows: rows})
	require.EqualValues(t, "running", m.worker.State)
	require.EqualValues(t, "updated alpha", m.rows[0].Sandbox.Name)
	frozen := m.View().Content
	require.Equal(t, ansi.Strip(frame), ansi.Strip(frozen), "observation changed the displayed selection frame")
	want := m.textSelection.text()
	_, command := m.Update(tea.MouseReleaseMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	assertClipboard(t, command, want)
	require.Equal(t, frozen, m.View().Content, "release erased the copied selection")
	m.Update(keyMsg('x', "x"))
	require.Nil(t, m.textSelection)
	require.Contains(t, m.View().Content, "updated alpha")
}

func TestDragPreservesInputsDialogsAndDetails(t *testing.T) {
	for _, context := range []string{"details", "filter", "rename", "dialog"} {
		t.Run(context, func(t *testing.T) {
			m := modelFixture(t)
			m.expanded, m.focus = "a", "actions"
			if context == "dialog" {
				m.dialog, m.button = "startup", 1
			} else if context != "details" {
				m.input = &inputState{kind: context, target: "a", text: "draft text", cursor: 4}
			}
			input := m.input
			m.View()
			m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
			m.Update(tea.MouseMotionMsg{X: 4, Y: 0, Button: tea.MouseLeft})
			_, command := m.Update(tea.MouseReleaseMsg{X: 4, Y: 0, Button: tea.MouseLeft})
			require.NotNil(t, command)
			require.False(t, m.input != input)
			require.EqualValues(t, "a", m.expanded)
			require.EqualValues(t, "actions", m.focus)
			require.False(t, input != nil && (input.text != "draft text" || input.cursor != 4), "drag changed input text or cursor")
			require.False(t, context == "dialog" && (m.dialog != "startup" || m.button != 1), "drag changed the active dialog")
		})
	}
}

func TestEscapeAndResizeCancelDragWithoutActivating(t *testing.T) {
	for _, cancel := range []string{"escape", "resize"} {
		m := modelFixture(t)
		m.expanded = "a"
		m.input = &inputState{kind: "rename", target: "a", text: "draft", cursor: 3}
		m.View()
		m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
		if cancel == "escape" {
			m.Update(keyMsg(tea.KeyEscape, ""))
		} else {
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
		}

		_, command := m.Update(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})
		require.Nil(t, command, "cancelled drag copied or activated on release")

		require.Nil(t, m.textSelection)
		require.NotNil(t, m.input)
		require.EqualValues(t, "draft", m.input.text)
		require.EqualValues(t, "a", m.expanded)
		require.Equal(t, tea.MouseMode(tea.MouseModeCellMotion), m.View().MouseMode, "cancelling drag disabled mouse navigation")
	}
}

func TestPointerAfterCopyUsesLatestLayout(t *testing.T) {
	m := modelFixture(t)
	h := selectionHit(t, m, "select", "b")
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 3, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 3, Y: 0, Button: tea.MouseLeft})
	m.rows[0].Sandbox.Name = "latest name"
	m.hits = []hit{{x: h.x, y: h.y, w: h.w, action: "select", id: "a"}}
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	require.NotNil(t, m.textSelection)
	require.Contains(t, m.textSelection.frame, "latest name")
	m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
}

func TestPendingClickDoesNotActivateAReplacementStatusAction(t *testing.T) {
	m := modelFixture(t)
	m.page = "Status"
	h := selectionHit(t, m, "status-action", "1") // Set up SSH.
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	// Once SSH is ready, Refresh replaces its numeric table position.
	m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready}}
	_, command := m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	require.Nil(t, command)
	require.EqualValues(t, "", m.dialog)
	require.False(t, m.refreshing)
}

func TestDragClampsEndpointsAndIgnoresScrollUntilRelease(t *testing.T) {
	m := modelFixture(t)
	m.renderedFrame = "alpha\nbeta"
	m.Update(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 1000, Y: 1000, Button: tea.MouseLeft})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	require.Equal(t, 0, m.scroll, "wheel scrolled the frozen frame while dragging")
	_, command := m.Update(tea.MouseReleaseMsg{X: 1000, Y: 1000, Button: tea.MouseNone})
	assertClipboard(t, command, "lpha\nbeta")
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	require.Nil(t, m.textSelection)
	require.EqualValues(t, 3, m.scroll)
}

func TestNonLeftPressDoesNotNavigateOrStartSelection(t *testing.T) {
	m := modelFixture(t)
	h := selectionHit(t, m, "select", "b")
	for _, button := range []tea.MouseButton{tea.MouseMiddle, tea.MouseRight} {
		m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: button})
		m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: button})
		require.Nil(t, m.textSelection)
		require.EqualValues(t, "a", m.selected)
		require.EqualValues(t, "a", m.expanded)
	}
}

func TestSelectionRemainsVisibleWithoutColor(t *testing.T) {
	m := modelFixture(t)
	t.Setenv("NO_COLOR", "")
	m.width, m.height = 60, 18
	m.View()
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 4, Y: 0, Button: tea.MouseLeft})
	view := m.View()
	require.Contains(t, view.Content, "\x1b[7m")
	require.False(t, view.MouseMode != tea.MouseModeCellMotion)
	for _, line := range strings.Split(view.Content, "\n") {
		require.Equal(t, 60, ansi.StringWidth(line), "selection changed narrow terminal geometry")
	}
}
