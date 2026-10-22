package panel

import (
	"context"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/store"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
)

func TestSandboxPreviewNavigationAndFocusedDetails(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
	if m.selected != "a" || m.expanded != "a" || m.focus != "table" {
		t.Fatal("initial preview missing")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.selected != "b" || m.expanded != "b" || m.focus != "table" {
		t.Fatal("Down entered details instead of next sandbox")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	if m.selected != "a" || m.expanded != "a" {
		t.Fatal("Up did not follow selection")
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.focus != "actions" || m.button != 0 {
		t.Fatal("Enter did not focus primary action")
	}
	m.key(keyMsg(tea.KeyRight, ""))
	if m.page != "Sandboxes" || m.button != 1 {
		t.Fatal("Right switched tabs or missed action")
	}
	m.key(keyMsg(tea.KeyLeft, ""))
	if m.button != 0 {
		t.Fatal("Left missed primary action")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	if m.focus != "actions" || m.selected != "a" {
		t.Fatal("Up escaped first details row")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "plugins" || m.detailIndex != 0 {
		t.Fatal("Down did not enter first plugin")
	}
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "plugins" || m.detailIndex != 1 || m.selected != "a" {
		t.Fatal("Down escaped last plugin")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	if m.focus != "actions" || m.button != 0 {
		t.Fatal("Up did not return to action row")
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.focus != "table" || m.expanded != "a" {
		t.Fatal("Escape removed preview")
	}
}

func TestDetailsHomeEndAndNoPluginsStayInside(t *testing.T) {
	for _, withPlugins := range []bool{false, true} {
		m := modelFixture(t)
		if withPlugins {
			m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
		}
		m.key(keyMsg(tea.KeyEnter, ""))
		m.key(keyMsg(tea.KeyEnd, ""))
		if withPlugins {
			if m.focus != "plugins" || m.detailIndex != 1 {
				t.Fatal("End did not reach last plugin")
			}
		} else {
			if m.focus != "actions" || m.button != len(m.sandboxActions(m.rows[0]))-1 {
				t.Fatal("End did not reach last action")
			}
		}
		m.key(keyMsg(tea.KeyDown, ""))
		if m.selected != "a" || m.focus == "table" {
			t.Fatal("Down escaped details")
		}
		m.key(keyMsg(tea.KeyHome, ""))
		if m.focus != "actions" || m.button != 0 {
			t.Fatal("Home missed primary action")
		}
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
		if !strings.Contains(header, "✓ alpha") || ansi.StringWidth(header) != width-4 {
			t.Fatalf("wrong header: %q", header)
		}
		if width == 110 && !strings.Contains(header, "5m ago") {
			t.Fatal("wide header lost observation age")
		}
		if !strings.Contains(details[0].text, colorSuccess) {
			t.Fatal("runtime lost semantic color")
		}
		card := ansi.Strip(strings.Join(lineTexts(details), "\n"))
		for _, text := range []string{"API desk", "? Stale", "observation", "running (stale)"} {
			if !strings.Contains(card, text) {
				t.Fatalf("summary lost %q: %s", text, card)
			}
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
		native := true
		m.rows[0].NativeEnabled = &native
		m.width = width
		m.rows[0].Status.Connection = "ready"
		m.focus = "actions"
		m.button = 2
		lines := m.sandboxDetails(m.rows[0])
		labels := map[string]string{"disable": "Disable", "rename": "Rename"}
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
	native := true
	m.rows[0].NativeEnabled = &native
	m.rows[0].Status.Connection = "ready"
	for _, tc := range []struct {
		name string
		row  int
		want []string
	}{
		{"enabled ready", 0, []string{"disable", "rename"}},
		{"disabled", 1, []string{"enable", "remove", "rename"}},
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
	if !reflect.DeepEqual(recovery, []string{"disable", "resync", "rename"}) {
		t.Fatalf("failed sandbox actions = %v", recovery)
	}
}

func TestDisabledDetailsSeparateAndIndentNotes(t *testing.T) {
	m := modelFixture(t)
	unsetNoColor(t)
	m.width = 60
	m.selected, m.expanded = "b", "b"
	r := m.rows[1]
	r.Status.Detail = "The guest registration needs review before sandbox management can begin."
	lines := m.sandboxDetails(r)
	card := ansi.Strip(strings.Join(lineTexts(lines), "\n"))
	if strings.Contains(card, r.Status.Detail) {
		t.Fatal("guidance belongs in Hint")
	}
	firstAction, pluginIndex, identityIndex := -1, -1, -1
	for i, l := range lines {
		plain := ansi.Strip(l.text)
		if strings.Trim(plain, " │") == "b" {
			identityIndex = i
		}
		if strings.Contains(plain, "╭─ Plugins") {
			pluginIndex = i
		}
		for _, h := range l.hits {
			if h.action == "action" && h.id == "enable" {
				firstAction = i
				bg := strings.Replace(colorAccent, "[38;", "[48;", 1)
				if !strings.Contains(l.text, bg) || strings.TrimSpace(ansi.Cut(plain, h.x, h.x+h.w)) != "Enable (space)" {
					t.Fatalf("wrong primary action: %q %#v", l.text, h)
				}
			}
		}
	}
	if firstAction < 1 || identityIndex <= firstAction || pluginIndex <= identityIndex {
		t.Fatal("actions, identity, plugins order lost")
	}
	if strings.Trim(ansi.Strip(lines[firstAction-1].text), " │") != "" {
		t.Fatal("header and actions lack separator")
	}
	if strings.Trim(ansi.Strip(lines[identityIndex-1].text), " │") != "" {
		t.Fatal("actions and identity lack separator")
	}
	m.focus, m.button = "actions", 0
	focused := m.sandboxDetails(r)
	for _, l := range focused {
		for _, h := range l.hits {
			if h.action == "action" && h.id == "enable" && strings.Contains(l.text, "\x1b[48;2;136;162;238m") {
				return
			}
		}
	}
	t.Fatal("focused primary button missing")
}

func TestSpaceRunsPrimaryWhileEnterRunsSelectedAction(t *testing.T) {
	m := modelFixture(t)
	m.key(keyMsg(tea.KeyEnter, ""))
	actions := m.sandboxActions(m.rows[0])
	for i, a := range actions {
		if a.id == "rename" {
			m.button = i
		}
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	if m.input == nil || m.input.kind != "rename" {
		t.Fatal("Enter did not activate selected secondary")
	}
	m.key(keyMsg(tea.KeyEscape, ""))
	calls := 0
	m.setEnabledFn = func(_ context.Context, scope, id string, choice store.EnabledChoice) error {
		calls++
		if choice != store.Disabled {
			t.Fatal("Space did not toggle primary")
		}
		return store.SetEnabled(scope, id, choice)
	}
	cmd := m.key(keyMsg(tea.KeySpace, " "))
	if cmd == nil || m.input != nil {
		t.Fatal("Space activated secondary instead of primary")
	}
	m.refreshing = true
	m.Update(cmd())
	if calls != 1 {
		t.Fatal("Space dispatched wrong number of actions")
	}
}

func TestDetailsHorizontalEntryAndReturnPreservesPreviewAndRenameInput(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
	for _, entry := range []rune{tea.KeyEnter, tea.KeyRight} {
		m.key(keyMsg(entry, ""))
		if m.focus != "actions" || m.button != 0 || m.page != "Sandboxes" {
			t.Fatal("entry did not focus primary action")
		}
		m.key(keyMsg(tea.KeyRight, ""))
		if m.focus != "actions" || m.button != 1 {
			t.Fatal("Right did not advance action")
		}
		m.key(keyMsg(tea.KeyLeft, ""))
		if m.focus != "actions" || m.button != 0 {
			t.Fatal("Left exited before primary action")
		}
		m.key(keyMsg(tea.KeyLeft, ""))
		if m.focus != "table" || m.selected != "a" || m.expanded != "a" {
			t.Fatal("Left at primary did not return to list preview")
		}
	}
	m.key(keyMsg(tea.KeyRight, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	if m.focus != "plugins" {
		t.Fatal("Down did not enter plugins")
	}
	m.key(keyMsg(tea.KeyRight, ""))
	if m.focus != "plugins" || m.detailIndex != 0 {
		t.Fatal("Right changed plugin focus")
	}
	m.key(keyMsg(tea.KeyLeft, ""))
	if m.focus != "table" || m.expanded != "a" {
		t.Fatal("Left did not return from plugin controls")
	}
	m.key(keyMsg(tea.KeyRight, ""))
	m.key(keyMsg(tea.KeyEscape, ""))
	if m.focus != "table" || m.expanded != "a" {
		t.Fatal("Escape no longer returns to list")
	}
	m.beginRename()
	m.input.text, m.input.cursor = "label", 2
	m.key(keyMsg(tea.KeyRight, ""))
	if m.input == nil || m.input.cursor != 3 || m.page != "Sandboxes" {
		t.Fatal("Right escaped rename instead of editing cursor")
	}
	m.key(keyMsg(tea.KeyLeft, ""))
	if m.input == nil || m.input.cursor != 2 || m.expanded != "a" {
		t.Fatal("Left escaped rename instead of editing cursor")
	}
}
