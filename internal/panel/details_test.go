package panel

import (
	"github.com/charmbracelet/x/ansi"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
)

func TestExpandedArrowNavigationStaysWithinDetails(t *testing.T) {
	t.Run("Enter then Down enters plugins", func(t *testing.T) {
		m := modelFixture(t)
		m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
		m.key(keyMsg(tea.KeyEnter, ""))
		m.key(keyMsg(tea.KeyDown, ""))
		if m.selected != "a" || m.expanded != "a" || m.focus != "plugins" || m.detailIndex != 0 {
			t.Fatalf("selected=%s expanded=%s focus=%s plugin=%d", m.selected, m.expanded, m.focus, m.detailIndex)
		}
	})
	t.Run("actions Down stays within sandbox", func(t *testing.T) {
		m := modelFixture(t)
		m.expanded = "a"
		m.focus = "actions"
		m.button = 0
		m.key(keyMsg(tea.KeyDown, ""))
		if m.selected != "a" || m.expanded != "a" || m.focus != "actions" || m.button != 1 {
			t.Fatalf("selected=%s expanded=%s focus=%s action=%d", m.selected, m.expanded, m.focus, m.button)
		}
	})
	t.Run("no plugins enters actions", func(t *testing.T) {
		m := modelFixture(t)
		m.key(keyMsg(tea.KeyEnter, ""))
		m.key(keyMsg(tea.KeyDown, ""))
		if m.selected != "a" || m.focus != "actions" || m.button != 0 {
			t.Fatalf("selected=%s focus=%s action=%d", m.selected, m.focus, m.button)
		}
	})
}

func TestExpandedNavigationTraversesPluginsActionsAndReturns(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
	m.key(keyMsg(tea.KeyEnter, ""))
	lastAction := len(m.sandboxActions(m.rows[0])) - 1
	for _, step := range []struct {
		key   rune
		focus string
		index int
	}{{tea.KeyDown, "plugins", 0}, {tea.KeyDown, "plugins", 1}, {tea.KeyDown, "actions", 0}, {tea.KeyDown, "actions", 1}, {tea.KeyUp, "actions", 0}, {tea.KeyUp, "plugins", 1}, {tea.KeyHome, "plugins", 0}, {tea.KeyUp, "table", 0}, {tea.KeyDown, "plugins", 0}, {tea.KeyEnd, "plugins", 1}, {tea.KeyDown, "actions", 0}, {tea.KeyEnd, "actions", lastAction}, {tea.KeyHome, "actions", 0}, {tea.KeyEnd, "actions", lastAction}} {
		m.key(keyMsg(step.key, ""))
		index := m.detailIndex
		if step.focus == "actions" {
			index = m.button
		}
		if m.selected != "a" || m.expanded != "a" || m.focus != step.focus || index != step.index {
			t.Fatalf("key=%v selected=%s expanded=%s focus=%s index=%d", step.key, m.selected, m.expanded, m.focus, index)
		}
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.selected != "b" || m.expanded != "a" || m.focus != "table" {
		t.Fatal("last action did not advance to next sandbox")
	}
}
func TestDetailsHeaderAndNestedPluginGeometry(t *testing.T) {
	for _, width := range []int{60, 110} {
		m := modelFixture(t)
		unsetNoColor(t)
		m.width, m.height = width, 18
		m.expanded = "a"
		m.rows[0].Status.Connection = "ready"
		m.rows[0].Status.Stale = true
		m.rows[0].Status.ObservedAt = time.Now().Add(-5 * time.Minute)
		m.plugins = []guestplugins.Choice{{ID: strings.Repeat("long.plugin.", 5)}, {ID: "second.plugin"}}
		details := m.sandboxDetails(m.rows[0])
		header := ansi.Strip(details[0].text)
		if !strings.Contains(header, "Details · a · Owned") || !strings.Contains(header, "5m ago") || !strings.HasSuffix(header, " running (stale) ╮") || ansi.StringWidth(header) != width-4 {
			t.Fatalf("wrong header: %q", header)
		}
		if !strings.Contains(details[0].text, strings.Replace(colorSuccess, "[38;", "[48;", 1)) {
			t.Fatal("runtime badge is not filled")
		}
		first := ansi.Strip(details[1].text)
		if !strings.Contains(first, "alpha → API desk (ready)") {
			t.Fatalf("wrong summary: %q", first)
		}
		for _, l := range details {
			if strings.Contains(ansi.Strip(l.text), "Sandbox:") || strings.Contains(ansi.Strip(l.text), "Runtime:") || strings.Contains(ansi.Strip(l.text), "Registration:") {
				t.Fatal("old details heading retained")
			}
			if ansi.StringWidth(l.text) != width-4 {
				t.Fatalf("nested width=%d want %d", ansi.StringWidth(l.text), width-4)
			}
		}
		m.focus = "plugins"
		m.detailIndex = 1
		m.reveal = true
		view := m.View().Content
		var target hit
		for _, h := range m.hits {
			if h.action == "override" && h.id == "second.plugin" {
				target = h
			}
		}
		if target.w == 0 {
			t.Fatal("selected plugin not visible")
		}
		rendered := strings.Split(ansi.Strip(view), "\n")[target.y]
		if ansi.Cut(rendered, target.x, target.x+2) != "> " {
			t.Fatalf("highlight/mouse prefix shifted: %q %#v", rendered, target)
		}
		selected := ansi.Cut(strings.Split(view, "\n")[target.y], target.x, target.x+target.w)
		if !strings.Contains(selected, backgroundSelected) || !strings.HasPrefix(ansi.Strip(selected), "> ") {
			t.Fatal("selected fill does not begin at plugin prefix")
		}
		command := m.click(target.x, target.y)
		if command == nil || m.dialog != "" || m.detailIndex != 1 {
			t.Fatal("nested plugin click targeted wrong choice")
		}
	}
}

func TestDetailActionsWrapWholeButtonsWithClickableTargets(t *testing.T) {
	for _, width := range []int{60, 110} {
		m := modelFixture(t)
		m.width = width
		m.rows[0].Status.Connection = "ready"
		m.focus = "actions"
		m.button = 2
		lines := m.sandboxDetails(m.rows[0])
		labels := map[string]string{"unwatch": "Unwatch", "disable": "Disable · remove from herdr", "rename": "Rename herdr label…"}
		seen := make(map[string]bool)
		for _, l := range lines {
			plain := ansi.Strip(l.text)
			for _, h := range l.hits {
				if h.action != "action" {
					continue
				}
				if h.x+h.w > ansi.StringWidth(plain) || !strings.Contains(ansi.Cut(plain, h.x, h.x+h.w), labels[h.id]) {
					t.Fatalf("width %d: action %s hit misses full button in %q", width, h.id, plain)
				}
				seen[h.id] = true
			}
		}
		if len(seen) != len(labels) {
			t.Fatalf("width %d: got %d action hits", width, len(seen))
		}
	}
}

func TestSandboxActionsAreContextual(t *testing.T) {
	m := modelFixture(t)
	m.rows[0].Status.Connection = "ready"
	for _, tc := range []struct {
		name string
		row  int
		want []string
	}{
		{"watched ready", 0, []string{"unwatch", "disable", "rename"}},
		{"unmanaged", 1, []string{"adopt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, action := range m.sandboxActions(m.rows[tc.row]) {
				got = append(got, action.id)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("actions = %v; want %v", got, tc.want)
			}
		})
	}
	m.rows[0].Status.Connection = "failed"
	var recovery []string
	for _, action := range m.sandboxActions(m.rows[0]) {
		recovery = append(recovery, action.id)
	}
	if !reflect.DeepEqual(recovery, []string{"unwatch", "disable", "resync", "rename"}) {
		t.Fatalf("failed sandbox actions = %v", recovery)
	}
}

func TestUnmanagedDetailsSeparateAndIndentNotes(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	m.width = 60
	r := m.rows[1]
	r.Status.Detail = "The guest registration needs review before sandbox management can begin."
	m.pending[r.Sandbox.ID] = true
	lines := m.sandboxDetails(r)
	if strings.TrimSpace(ansi.Cut(ansi.Strip(lines[2].text), 4, m.width-6)) != "" {
		t.Fatalf("summary has no blank line after it: %q", ansi.Strip(lines[2].text))
	}
	var notes strings.Builder
	for _, l := range lines[3:] {
		plain := ansi.Strip(l.text)
		if strings.Contains(plain, "╭─ Plugins") {
			break
		}
		if !strings.Contains(plain, "│") {
			continue
		}
		if ansi.Cut(plain, 4, 6) != "  " {
			t.Fatalf("note is not indented: %q", plain)
		}
		notes.WriteString(plain)
	}
	if got := notes.String(); strings.Contains(got, r.Status.Detail) {
		t.Fatalf("status guidance belongs in Hint: %q", got)
	}
	actions := m.sandboxActions(r)
	if len(actions) != 1 || actions[0].id != "adopt" || actions[0].label != "Adopt" {
		t.Fatalf("unmanaged actions = %+v", actions)
	}
	primaryBackground := strings.Replace(colorAccent, "[38;", "[48;", 1)
	var adoptLine line
	for _, l := range lines {
		for _, h := range l.hits {
			if h.action == "action" && h.id == "adopt" {
				adoptLine = l
				if !strings.Contains(l.text, primaryBackground) || ansi.Cut(ansi.Strip(l.text), h.x, h.x+h.w) != "  Adopt  " {
					t.Fatalf("Adopt lacks primary button or hit geometry: %q %#v", l.text, h)
				}
			}
		}
	}
	if adoptLine.text == "" {
		t.Fatal("Adopt button missing")
	}
	m.focus = "actions"
	if focused := m.sandboxDetails(r); !strings.Contains(focused[len(focused)-2].text, "\x1b[48;2;136;162;238m") {
		t.Fatal("focused Adopt button lacks selected fill")
	}
}
