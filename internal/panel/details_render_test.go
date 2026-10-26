package panel

import (
	"io"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestExpandedCardReplacesRowAndKeepsIdentityActionsAndVersion(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	m.width = 110
	m.expanded, m.selected = "a", "a"
	r := &m.rows[0]
	r.ProfileID = "machine-id"
	r.Status.Connection = "ready"
	r.Status.GuestServer = &store.GuestServerObservation{Version: "0.9.3"}
	m.plugins = []guestplugins.Choice{{ID: "example.plugin"}}
	lines := m.sandboxLines()
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(ansi.Strip(l.text))
		text.WriteByte('\n')
	}
	content := text.String()
	require.Equal(t, 1, strings.Count(content, "alpha"), "expanded row duplicated sandbox identity: %s", content)
	header := lines[3]
	require.Contains(t, ansi.Strip(header.text), "✓ alpha (running) → API desk (ready)")
	require.True(t, strings.HasSuffix(ansi.Strip(header.text), " herdr 0.9.3 ╮"))
	require.Contains(t, header.text, colorAccent)
	require.Contains(t, header.text, colorSand)
	require.Contains(t, header.text, colorSuccess)
	action, identity, plugins := strings.Index(content, "Disable (space)"), strings.Index(content, "a → machine-id"), strings.Index(content, "╭─ Plugins")
	require.False(t, action < 0)
	require.False(t, identity < action)
	require.False(t, plugins < identity)
	require.NotContains(t, m.paintLine(header), backgroundSelected, "preview fill erased semantic header colors")
	for _, focus := range []string{"table", "actions", "plugins"} {
		m.focus = focus
		details := m.sandboxDetails(*r)
		want := colorSeparator
		if focus != "table" {
			want = colorText
		}
		require.True(t, strings.HasPrefix(details[0].text, want+"  ╭"))
		require.True(t, strings.HasPrefix(details[len(details)-1].text, want+"  ╰"))
	}
}

func TestExpandedCardTitleKeepsCheckboxAndRenameHits(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		testExpandedCardTitle(t, width)
	}
}

func TestDetailFocusMutesPrimaryAndShowsRenameSaveState(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		testDetailFocus(t, width)
	}
}

func TestCompactDetailsPreserveWrappedLabelAndIdentityStyles(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width = 40
	row := m.rows[0]
	row.Sandbox.ID = "bbfd38ff-8151-4ca2-a2dd-ef04e0995857"
	row.ProfileID = "25a9b0f5f63eab13de0b68573e18cd92"
	row.Label = "sbx · claude-testcontainers"
	// Inspect the terminal pen at distinctive continuation fragments after the
	// real card has inserted each row's borders and padding.
	for fragment, want := range map[string]string{"testcontainers": colorSand, "5857": colorMuted, "cd92": colorMuted} {
		found := false
		for _, line := range m.sandboxDetails(row) {
			if index := strings.Index(line.text, fragment); index >= 0 {
				pen := lipgloss.NewWrapWriter(io.Discard)
				_, _ = pen.Write([]byte(line.text[:index]))
				got := pen.Style().Fg
				_ = pen.Close()
				expected := lipgloss.NewWrapWriter(io.Discard)
				_, _ = expected.Write([]byte(want))
				color := expected.Style().Fg
				_ = expected.Close()
				require.Equal(t, color, got, "%q continuation lost style: got=%v want=%v line=%q", fragment, got, color, line.text)
				found = true
			}
		}
		require.True(t, found, "fixture continuation %q not rendered", fragment)
	}
}

func TestWrappedDetailTitleKeepsPanelBackgroundBeforeVersionBadge(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.width, m.height = 64, 32
	m.rows[0].Sandbox.Name = "claude-testcontainers-integration-platform"
	m.rows[0].Label = "CM"
	m.rows[0].Status.Connection = "ready"
	m.rows[0].Status.GuestServer = &store.GuestServerObservation{Version: "0.9.3"}
	for _, focus := range []string{"table", "actions"} {
		m.focus = focus
		frame := m.View().Content
		found := false
		for _, row := range strings.Split(frame, "\n") {
			badge := strings.Index(row, "herdr 0.9.3")
			if badge < 0 {
				continue
			}
			fill := strings.LastIndex(row[:badge], "─")
			require.False(t, fill < 0, "fixture has no title-border filler")
			pen := lipgloss.NewWrapWriter(io.Discard)
			_, _ = pen.Write([]byte(row[:fill]))
			got := pen.Style().Bg
			_ = pen.Close()
			expected := lipgloss.NewWrapWriter(io.Discard)
			_, _ = expected.Write([]byte(background))
			want := expected.Style().Bg
			_ = expected.Close()
			require.Equal(t, want, got, "focus %s filler background=%v want=%v row=%q", focus, got, want, row)
			found = true
		}
		require.True(t, found, "fixture header/version missing")
	}
}

func TestFilledRestoresSurfaceAfterFullResetAndClearsTextAttributes(t *testing.T) {
	unsetNoColor(t)
	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		text := filled(colorText, background, "\x1b[1mBefore"+reset+"After")
		pen := lipgloss.NewWrapWriter(io.Discard)
		before, _, found := strings.Cut(text, "After")
		require.True(t, found)
		_, _ = pen.Write([]byte(before))
		got := pen.Style()
		_ = pen.Close()
		expected := lipgloss.NewWrapWriter(io.Discard)
		_, _ = expected.Write([]byte(colorText + background))
		want := expected.Style()
		_ = expected.Close()
		require.Equal(t, want, got, "reset %q: got=%v want=%v", reset, got, want)
	}
}

func testExpandedCardTitle(t *testing.T, width int) {
	t.Helper()
	m := modelFixture(t)
	m.width = width
	m.expanded, m.selected = "a", "a"
	m.input = &inputState{kind: "rename", target: "a", text: "New label", cursor: 9}
	details := m.sandboxDetails(m.rows[0])
	var all strings.Builder
	var checkbox, rename []hit
	for _, l := range details {
		all.WriteString(ansi.Strip(l.text))
	}
	checkbox = detailHits(details, func(h hit) bool { return h.action == "enabled" })
	rename = detailHits(details, func(h hit) bool { return h.action == "action" && h.id == "rename" })
	require.Len(t, checkbox, 1, "checkbox target missing at width %d", width)
	require.Equal(t, "a", checkbox[0].id)
	require.Len(t, rename, 1, "rename target missing at width %d", width)
	require.Contains(t, all.String(), "New label█", "inline rename lost at width %d", width)
	assertCardHitCells(t, details, width)

}

func testDetailFocus(t *testing.T, width int) {
	t.Helper()
	m := modelFixture(t)
	unsetNoColor(t)
	m.width = width
	m.selected, m.expanded = "a", "a"
	actionText := func(id string) string { return detailActionText(t, m, id, width) }

	isBlue := func(text string) bool {
		// ANSI cuts may retain zero-width color resets from the next button.
		text, _, _ = strings.Cut(text, "Disable")
		return strings.Contains(text, strings.Replace(colorAccent, "[38;", "[48;", 1)) || strings.Contains(text, "\x1b[48;2;136;162;238m") || strings.Contains(text, backgroundSelected)
	}
	for _, state := range []struct {
		focus  string
		button int
		blue   bool
	}{
		{"table", 0, true}, {"actions", 0, true}, {"actions", len(m.sandboxActions(m.rows[0])) - 1, false}, {"plugins", 0, false},
	} {
		m.focus, m.button = state.focus, state.button
		primary := actionText("disable")
		require.Equal(t, state.blue, isBlue(primary), "width %d focus %s button %d: primary emphasis %q", width, state.focus, state.button, primary)
		require.Contains(t, primary, "\x1b[22m(space)\x1b[1m", "hotkey weight changed: %q", primary)
	}
	m.focus, m.button = "table", 0
	m.input = &inputState{kind: "rename", target: "a", text: "New label", cursor: 3}
	rename := actionText("rename")
	require.Contains(t, rename, strings.Replace(colorSand, "[38;", "[48;", 1))
	require.Contains(t, ansi.Strip(rename), "Rename ↵")
	require.False(t, isBlue(actionText("disable")), "primary stayed emphasized during rename")

	got := m.renderedInput(40)
	require.Equal(t, "New█ label", got, "block cursor at wrong position: %q", got)

}

func detailHits(lines []line, match func(hit) bool) []hit {
	var found []hit
	for _, l := range lines {
		for _, h := range l.hits {
			if match(h) {
				found = append(found, h)
			}
		}
	}
	return found
}

func assertCardHitCells(t *testing.T, details []line, width int) {
	t.Helper()
	for _, l := range details {
		for _, h := range l.hits {
			require.GreaterOrEqual(t, h.x, 0)
			require.LessOrEqual(t, h.x+h.w, width-4)
			cell := ansi.Cut(ansi.Strip(l.text), h.x, h.x+h.w)
			if h.action == "enabled" {
				require.Equal(t, "✓", cell)
			}
			if h.action == "action" && h.id == "rename" {
				require.Equal(t, "Rename ↵", strings.TrimSpace(cell))
			}
		}
	}
}

func detailActionText(t *testing.T, m *model, id string, width int) string {

	t.Helper()
	for _, l := range m.sandboxDetails(m.rows[0]) {
		for _, h := range l.hits {
			if h.action == "action" && h.id == id {
				require.False(t, h.x+h.w > width-4, "action hit outside card: %+v", h)
				return ansi.Cut(l.text, h.x, h.x+h.w)
			}
		}
	}
	t.Fatalf("action %s missing", id)
	return ""
}
