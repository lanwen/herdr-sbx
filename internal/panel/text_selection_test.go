package panel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
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
	if command == nil {
		t.Fatal("clipboard command missing")
	}
	if got, expected := command(), tea.SetClipboard(want)(); got != expected {
		t.Fatalf("clipboard command = %#v, want %#v", got, expected)
	}
}

func TestMouseClickActivatesOriginalTargetOnceOnRelease(t *testing.T) {
	m := modelFixture(t)
	h := selectionHit(t, m, "select", "b")
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	if m.selected != "a" || m.expanded != "" {
		t.Fatal("mouse press activated the row before release")
	}
	// Even if a later observation provides different hit targets, release must
	// activate the one captured from the displayed frame at press time.
	m.hits = []hit{{x: h.x, y: h.y, w: h.w, action: "select", id: "a"}}
	release := tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft}
	m.Update(release)
	if m.selected != "b" || m.expanded != "b" {
		t.Fatal("release activated a different target")
	}
	m.Update(release)
	if m.expanded != "b" {
		t.Fatal("duplicate release activated the row twice")
	}
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
		if m.selected != "a" || m.expanded != "" || m.textSelection == nil || m.textSelection.pressed {
			t.Fatal("drag activated a row or lost its completed highlight")
		}
		view := m.View()
		if view.MouseMode != tea.MouseModeCellMotion || !strings.Contains(view.Content, "\x1b[7m") || ansi.Strip(view.Content) != "alpha" {
			t.Fatal("drag changed content, lacked reverse-video selection, or released mouse capture")
		}
		if _, command := m.Update(tea.MouseReleaseMsg{X: end, Y: 0, Button: tea.MouseLeft}); command != nil {
			t.Fatal("duplicate release copied again")
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
			if got := s.text(); got != test.want {
				t.Fatalf("copied %q, want %q", got, test.want)
			}
			if ansi.Strip(s.view()) != ansi.Strip(frame) || !strings.Contains(s.view(), "\x1b[7m") {
				t.Fatal("selection highlight changed displayed characters")
			}
			original, highlighted := strings.Split(frame, "\n"), strings.Split(s.view(), "\n")
			for y := range original {
				if ansi.StringWidth(original[y]) != ansi.StringWidth(highlighted[y]) {
					t.Fatal("selection highlight changed cell geometry")
				}
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
	rows := append(m.rows[:0:0], m.rows...)
	rows[0].Sandbox.Name = "updated alpha"
	m.Update(observationMsg{generation: m.generation, rows: rows})
	if m.worker.State != "running" || m.rows[0].Sandbox.Name != "updated alpha" {
		t.Fatal("drag blocked background observations")
	}
	frozen := m.View().Content
	if ansi.Strip(frozen) != ansi.Strip(frame) {
		t.Fatal("observation changed the displayed selection frame")
	}
	want := m.textSelection.text()
	_, command := m.Update(tea.MouseReleaseMsg{X: h.x + 3, Y: h.y, Button: tea.MouseLeft})
	assertClipboard(t, command, want)
	if m.View().Content != frozen {
		t.Fatal("release erased the copied selection")
	}
	m.Update(keyMsg('x', "x"))
	if m.textSelection != nil || !strings.Contains(m.View().Content, "updated alpha") {
		t.Fatal("next interaction did not restore latest observations")
	}
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
			if command == nil || m.input != input || m.expanded != "a" || m.focus != "actions" {
				t.Fatal("drag changed active input, focus, or details")
			}
			if input != nil && (input.text != "draft text" || input.cursor != 4) {
				t.Fatal("drag changed input text or cursor")
			}
			if context == "dialog" && (m.dialog != "startup" || m.button != 1) {
				t.Fatal("drag changed the active dialog")
			}
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
		if _, command := m.Update(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft}); command != nil {
			t.Fatal("cancelled drag copied or activated on release")
		}
		if m.textSelection != nil || m.input == nil || m.input.text != "draft" || m.expanded != "a" {
			t.Fatal("cancelling drag changed the active editor or details")
		}
		if m.View().MouseMode != tea.MouseModeCellMotion {
			t.Fatal("cancelling drag disabled mouse navigation")
		}
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
	if m.textSelection == nil || !strings.Contains(m.textSelection.frame, "latest name") {
		t.Fatal("next mouse press did not render latest layout")
	}
	m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	if m.selected != "b" || m.expanded != "b" {
		t.Fatal("next click used stale hit targets")
	}
}

func TestPendingClickDoesNotActivateAReplacementStatusAction(t *testing.T) {
	m := modelFixture(t)
	m.page = "Status"
	h := selectionHit(t, m, "status-action", "1") // Set up SSH.
	m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	// Once SSH is ready, Refresh replaces its numeric table position.
	m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready}}
	_, command := m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: tea.MouseLeft})
	if command != nil || m.dialog != "" || m.refreshing {
		t.Fatal("release activated the replacement status action")
	}
}

func TestDragClampsEndpointsAndIgnoresScrollUntilRelease(t *testing.T) {
	m := modelFixture(t)
	m.renderedFrame = "alpha\nbeta"
	m.Update(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 1000, Y: 1000, Button: tea.MouseLeft})
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.scroll != 0 {
		t.Fatal("wheel scrolled the frozen frame while dragging")
	}
	_, command := m.Update(tea.MouseReleaseMsg{X: 1000, Y: 1000, Button: tea.MouseNone})
	assertClipboard(t, command, "lpha\nbeta")
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.textSelection != nil || m.scroll != 3 {
		t.Fatal("wheel did not clear completed selection and resume scrolling")
	}
}

func TestNonLeftPressDoesNotNavigateOrStartSelection(t *testing.T) {
	m := modelFixture(t)
	h := selectionHit(t, m, "select", "b")
	for _, button := range []tea.MouseButton{tea.MouseMiddle, tea.MouseRight} {
		m.Update(tea.MouseClickMsg{X: h.x, Y: h.y, Button: button})
		m.Update(tea.MouseReleaseMsg{X: h.x, Y: h.y, Button: button})
		if m.textSelection != nil || m.selected != "a" || m.expanded != "" {
			t.Fatal("non-left press started selection or activated a control")
		}
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
	if !strings.Contains(view.Content, "\x1b[7m") || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("selection lost reverse-video visibility without color")
	}
	for _, line := range strings.Split(view.Content, "\n") {
		if ansi.StringWidth(line) != 60 {
			t.Fatal("selection changed narrow terminal geometry")
		}
	}
}
