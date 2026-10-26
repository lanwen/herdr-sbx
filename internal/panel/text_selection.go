package panel

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type selectionPoint struct{ x, y int }

// textSelection retains the displayed frame, not the live model. Background
// observations continue while dragging and while the copied text is highlighted.
type textSelection struct {
	frame            string
	anchor, end      selectionPoint
	pressed, dragged bool
	target           *hit
	page, dialog     string
	dialogTarget     string
	expanded         string
	statusAction     string
	errorTarget      *uiError
}

func (m *model) clearTextSelection() {
	if m.textSelection != nil {
		m.textSelection = nil
		// The next pointer action must use the latest layout, not frozen hits.
		m.View()
	}
}

func (m *model) beginTextSelection(x, y int) {
	if m.renderedFrame == "" {
		m.View()
	}
	s := &textSelection{frame: m.renderedFrame, pressed: true, page: m.page, dialog: m.dialog, dialogTarget: m.dialogTarget, expanded: m.expanded}
	s.anchor = s.point(x, y)
	s.end = s.anchor
	m.bindSelectionTarget(s, x, y)
	m.textSelection = s
}

func (s *textSelection) point(x, y int) selectionPoint {
	lines := strings.Split(s.frame, "\n")
	y = min(max(0, y), len(lines)-1)
	x = min(max(0, x), max(0, ansi.StringWidth(lines[y])-1))
	return selectionPoint{x, y}
}

func (s *textSelection) move(x, y int) {
	s.end = s.point(x, y)
	// Once the pointer moves a cell, returning to the anchor is still a drag.
	s.dragged = s.dragged || x != s.anchor.x || y != s.anchor.y
}

func (m *model) releaseTextSelection(x, y int) tea.Cmd {
	s := m.textSelection
	if s == nil || !s.pressed {
		return nil
	}
	s.move(x, y)
	s.pressed = false
	if s.dragged {
		if text := s.text(); text != "" {
			return tea.SetClipboard(text)
		}
		return nil
	}
	m.textSelection = nil
	// A poll during the press must not turn a click into a different action.
	if s.target != nil && m.page == s.page && m.dialog == s.dialog && m.dialogTarget == s.dialogTarget && m.expanded == s.expanded {
		return m.releaseSelectionTarget(s)
	}
	return nil
}

func (s *textSelection) bounds() (selectionPoint, selectionPoint) {
	start, end := s.anchor, s.end
	if start.y > end.y || start.y == end.y && start.x > end.x {
		start, end = end, start
	}
	return start, end
}

// selectionColumns expands cell endpoints to include whole graphemes, including
// a wide character when the pointer rests on either of its terminal cells.
func selectionColumns(line string, left, right int) (int, int) {
	x := 0
	for text := ansi.Strip(line); text != ""; {
		cluster, width := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
		if x < left && left < x+width {
			left = x
		}
		if x < right && right < x+width {
			right = x + width
		}
		x += width
		text = text[len(cluster):]
	}
	return left, right
}

func (s *textSelection) lineColumns(line string, y int) (int, int) {
	start, end := s.bounds()
	left, right := 0, ansi.StringWidth(line)
	if y == start.y {
		left = start.x
	}
	if y == end.y {
		right = end.x + 1
	}
	return selectionColumns(line, left, right)
}

func (s *textSelection) text() string {
	start, end := s.bounds()
	lines := strings.Split(s.frame, "\n")
	var selected []string
	for y := start.y; y <= end.y; y++ {
		left, right := s.lineColumns(lines[y], y)
		text := ansi.Strip(ansi.Cut(lines[y], left, right))
		selected = append(selected, strings.TrimRight(text, " "))
	}
	return strings.Join(selected, "\n")
}

func (s *textSelection) view() string {
	if !s.dragged {
		return s.frame
	}
	start, end := s.bounds()
	lines := strings.Split(s.frame, "\n")
	for y := start.y; y <= end.y; y++ {
		line := lines[y]
		left, right := s.lineColumns(line, y)
		// Strip styles inside the selected span so embedded resets cannot hide
		// reverse video. The unselected spans retain their original styling.
		lines[y] = ansi.Cut(line, 0, left) + "\x1b[7m" + ansi.Strip(ansi.Cut(line, left, right)) + "\x1b[27m" + ansi.Cut(line, right, ansi.StringWidth(line))
	}
	return strings.Join(lines, "\n")
}

func (m *model) bindSelectionTarget(s *textSelection, x, y int) {
	for _, h := range m.hits {
		if y != h.y || x < h.x || x >= h.x+h.w {
			continue
		}
		{
			s.target = &h
			index, _ := strconv.Atoi(h.id)
			if h.action == "status-action" && index >= 0 && index < len(m.statusActions()) {
				s.statusAction = m.statusActions()[index].id
			}
			if entries := m.visibleErrors(); h.action == "error" && index >= 0 && index < len(entries) {
				error := entries[index].uiError
				s.errorTarget = &error
			}
			break
		}
	}
}

func (m *model) releaseSelectionTarget(s *textSelection) tea.Cmd {
	target := *s.target
	switch target.action {
	case "status-action":
		for index, action := range m.statusActions() {
			if action.id == s.statusAction {
				target.id = strconv.Itoa(index)
				return m.hitAction(target)
			}
		}
		return nil
	case "error":
		return m.releaseDiagnosticTarget(s, target)
	}
	return m.hitAction(target)
}

func (m *model) releaseDiagnosticTarget(s *textSelection, target hit) tea.Cmd {
	for index, current := range m.visibleErrors() {
		if s.errorTarget != nil && current.code == s.errorTarget.code && current.scope == s.errorTarget.scope {
			target.id = strconv.Itoa(index)
			return m.hitAction(target)
		}
	}
	return nil
}
