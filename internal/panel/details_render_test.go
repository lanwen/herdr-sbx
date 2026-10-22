package panel

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
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
	if strings.Count(content, "alpha") != 1 {
		t.Fatalf("expanded row duplicated sandbox identity: %s", content)
	}
	header := lines[3]
	if !strings.Contains(ansi.Strip(header.text), "✓ alpha (running) → API desk (ready)") || !strings.HasSuffix(ansi.Strip(header.text), " herdr 0.9.3 ╮") {
		t.Fatalf("identity/version missing from title: %q", header.text)
	}
	if !strings.Contains(header.text, colorAccent) || !strings.Contains(header.text, colorSand) || !strings.Contains(header.text, colorSuccess) {
		t.Fatalf("semantic title colors missing: %q", header.text)
	}
	action, identity, plugins := strings.Index(content, "Disable (space)"), strings.Index(content, "a → machine-id"), strings.Index(content, "╭─ Plugins")
	if action < 0 || identity < action || plugins < identity {
		t.Fatalf("actions/identity/plugins order: %s", content)
	}
	if strings.Contains(m.paintLine(header), backgroundSelected) {
		t.Fatal("preview fill erased semantic header colors")
	}
	for _, focus := range []string{"table", "actions", "plugins"} {
		m.focus = focus
		details := m.sandboxDetails(*r)
		want := colorSeparator
		if focus != "table" {
			want = colorText
		}
		if !strings.HasPrefix(details[0].text, want+"  ╭") || !strings.HasPrefix(details[len(details)-1].text, want+"  ╰") {
			t.Fatalf("focus %s: wrong outline emphasis", focus)
		}
	}
}

func TestExpandedCardTitleKeepsCheckboxAndRenameHits(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		m := modelFixture(t)
		m.width = width
		m.expanded, m.selected = "a", "a"
		m.input = &inputState{kind: "rename", target: "a", text: "New label", cursor: 9}
		details := m.sandboxDetails(m.rows[0])
		var all strings.Builder
		for _, l := range details {
			all.WriteString(ansi.Strip(l.text))
			for _, h := range l.hits {
				if h.x < 0 || h.x+h.w > width-4 {
					t.Fatalf("out of bounds hit: %+v", h)
				}
			}
		}
		if !strings.Contains(all.String(), "New label█") {
			t.Fatalf("inline rename lost at width %d: %s", width, all.String())
		}
		for _, h := range details[0].hits {
			if h.action == "enabled" && ansi.Cut(ansi.Strip(details[0].text), h.x, h.x+h.w) != "✓" {
				t.Fatalf("checkbox hit shifted: %+v %q", h, details[0].text)
			}
		}
	}
}

func TestDetailFocusMutesPrimaryAndShowsRenameSaveState(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		m := modelFixture(t)
		unsetNoColor(t)
		m.width = width
		m.selected, m.expanded = "a", "a"
		actionText := func(id string) string {
			t.Helper()
			for _, l := range m.sandboxDetails(m.rows[0]) {
				for _, h := range l.hits {
					if h.action == "action" && h.id == id {
						if h.x+h.w > width-4 {
							t.Fatalf("action hit outside card: %+v", h)
						}
						return ansi.Cut(l.text, h.x, h.x+h.w)
					}
				}
			}
			t.Fatalf("action %s missing", id)
			return ""
		}
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
			if isBlue(primary) != state.blue {
				t.Fatalf("width %d focus %s button %d: primary emphasis %q", width, state.focus, state.button, primary)
			}
			if !strings.Contains(primary, "\x1b[22m(space)\x1b[1m") {
				t.Fatalf("hotkey weight changed: %q", primary)
			}
		}
		m.focus, m.button = "table", 0
		m.input = &inputState{kind: "rename", target: "a", text: "New label", cursor: 3}
		rename := actionText("rename")
		if !strings.Contains(rename, strings.Replace(colorSand, "[38;", "[48;", 1)) || !strings.Contains(ansi.Strip(rename), "Rename ↵") {
			t.Fatalf("rename save state missing: %q", rename)
		}
		if isBlue(actionText("disable")) {
			t.Fatal("primary stayed emphasized during rename")
		}
		if got := m.renderedInput(40); got != "New█ label" {
			t.Fatalf("block cursor at wrong position: %q", got)
		}
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
				if !reflect.DeepEqual(got, color) {
					t.Fatalf("%q continuation lost style: got=%v want=%v line=%q", fragment, got, color, line.text)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("fixture continuation %q not rendered", fragment)
		}
	}
}
