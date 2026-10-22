package panel

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSandboxPreviewNavigationAndFocusedDetails(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	require.EqualValues(t, "table", m.focus)
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "table", m.focus)
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, 0, m.button)
	m.key(keyMsg(tea.KeyRight, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, 1, m.button)
	m.key(keyMsg(tea.KeyLeft, ""))
	require.Equal(t, 0, m.button, "Left missed primary action")
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, "a", m.selected)
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "plugins", m.focus)
	require.EqualValues(t, 0, m.detailIndex)
	m.key(keyMsg(tea.KeyDown, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	require.EqualValues(t, "plugins", m.focus)
	require.EqualValues(t, 1, m.detailIndex)
	require.EqualValues(t, "a", m.selected)
	m.key(keyMsg(tea.KeyUp, ""))
	m.key(keyMsg(tea.KeyUp, ""))
	require.EqualValues(t, "actions", m.focus)
	require.EqualValues(t, 0, m.button)
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "a", m.expanded)
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
			require.EqualValues(t, "plugins", m.focus)
			require.EqualValues(t, 1, m.detailIndex)
		} else {
			require.EqualValues(t, "actions", m.focus)
			require.False(t, m.button != len(m.sandboxActions(m.rows[0]))-1)
		}
		m.key(keyMsg(tea.KeyDown, ""))
		require.EqualValues(t, "a", m.selected)
		require.NotEqualValues(t, "table", m.focus)
		m.key(keyMsg(tea.KeyHome, ""))
		require.EqualValues(t, "actions", m.focus)
		require.EqualValues(t, 0, m.button)
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
		require.Contains(t, header, "✓ alpha")
		require.False(t, ansi.StringWidth(header) != width-4)
		require.False(t, width == 110 && !strings.Contains(header, "5m ago"), "wide header lost observation age")
		require.Contains(t, details[0].text, colorSuccess, "runtime lost semantic color")
		card := ansi.Strip(strings.Join(lineTexts(details), "\n"))
		for _, text := range []string{"API desk", "? Stale", "observation", "running (stale)"} {
			require.Contains(t, card, text, "summary lost %q: %s", text, card)
		}

		for _, l := range details {
			require.NotContains(t, ansi.Strip(l.text), "Sandbox:")
			require.NotContains(t, ansi.Strip(l.text), "Runtime:")
			require.NotContains(t, ansi.Strip(l.text), "Registration:")
			require.Equal(t, width-4, ansi.StringWidth(l.text), "nested width=%d want %d", ansi.StringWidth(l.text), width-4)
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
		require.NotEqual(t, 0, target.w, "selected plugin not visible")
		rendered := strings.Split(ansi.Strip(view), "\n")[target.y]
		require.Equal(t, "> ", ansi.Cut(rendered, target.x, target.x+2), "highlight/mouse prefix shifted: %q %#v", rendered, target)
		selected := ansi.Cut(strings.Split(view, "\n")[target.y], target.x, target.x+target.w)
		require.Contains(t, selected, backgroundSelected)
		require.True(t, strings.HasPrefix(ansi.Strip(selected), "> "))
		command := clickAt(m, target.x, target.y)
		require.NotNil(t, command)
		require.EqualValues(t, "", m.dialog)
		require.EqualValues(t, 1, m.detailIndex)
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
				require.False(t, h.x+h.w > ansi.StringWidth(plain))
				require.Contains(t, ansi.Cut(plain, h.x, h.x+h.w), labels[h.id])
				seen[h.id] = true
			}
		}
		require.Len(t, seen, len(labels), "width %d: got %d action hits", width, len(seen))
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
			require.Equal(t, tc.want, got, "actions = %v; want %v", got, tc.want)
		})
	}
	m.rows[0].Status.Connection = "failed"
	var recovery []string
	for _, action := range m.sandboxActions(m.rows[0]) {
		recovery = append(recovery, action.id)
	}
	require.Equal(t, []string{"disable", "resync", "rename"}, recovery, "failed sandbox actions = %v", recovery)
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
	require.NotContains(t, card, r.Status.Detail, "guidance belongs in Hint")
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
				require.Contains(t, l.text, bg)
				require.EqualValues(t, "Enable (space)", strings.TrimSpace(ansi.Cut(plain, h.x, h.x+h.w)))
			}
		}
	}
	require.False(t, firstAction < 1)
	require.False(t, identityIndex <= firstAction)
	require.False(t, pluginIndex <= identityIndex)
	require.Equal(t, "", strings.Trim(ansi.Strip(lines[firstAction-1].text), " │"), "header and actions lack separator")
	require.Equal(t, "", strings.Trim(ansi.Strip(lines[identityIndex-1].text), " │"), "actions and identity lack separator")
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
	require.NotNil(t, m.input)
	require.EqualValues(t, "rename", m.input.kind)
	m.key(keyMsg(tea.KeyEscape, ""))
	calls := 0
	m.setEnabledFn = func(_ context.Context, scope, id string, choice store.EnabledChoice) error {
		calls++
		require.Equal(t, store.EnabledChoice(store.Disabled), choice, "Space did not toggle primary")
		return store.SetEnabled(scope, id, choice)
	}
	cmd := m.key(keyMsg(tea.KeySpace, " "))
	require.NotNil(t, cmd)
	require.Nil(t, m.input)
	m.refreshing = true
	m.Update(cmd())
	require.Equal(t, 1, calls, "Space dispatched wrong number of actions")
}

func TestDetailsHorizontalEntryAndReturnPreservesPreviewAndRenameInput(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "one"}, {ID: "two"}}
	for _, entry := range []rune{tea.KeyEnter, tea.KeyRight} {
		m.key(keyMsg(entry, ""))
		require.EqualValues(t, "actions", m.focus)
		require.EqualValues(t, 0, m.button)
		require.EqualValues(t, "Sandboxes", m.page)
		m.key(keyMsg(tea.KeyRight, ""))
		require.EqualValues(t, "actions", m.focus)
		require.EqualValues(t, 1, m.button)
		m.key(keyMsg(tea.KeyLeft, ""))
		require.EqualValues(t, "actions", m.focus)
		require.EqualValues(t, 0, m.button)
		m.key(keyMsg(tea.KeyLeft, ""))
		require.EqualValues(t, "table", m.focus)
		require.EqualValues(t, "a", m.selected)
		require.EqualValues(t, "a", m.expanded)
	}
	m.key(keyMsg(tea.KeyRight, ""))
	m.key(keyMsg(tea.KeyDown, ""))
	require.Equal(t, "plugins", m.focus, "Down did not enter plugins")
	m.key(keyMsg(tea.KeyRight, ""))
	require.EqualValues(t, "plugins", m.focus)
	require.EqualValues(t, 0, m.detailIndex)
	m.key(keyMsg(tea.KeyLeft, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "a", m.expanded)
	m.key(keyMsg(tea.KeyRight, ""))
	m.key(keyMsg(tea.KeyEscape, ""))
	require.EqualValues(t, "table", m.focus)
	require.EqualValues(t, "a", m.expanded)
	m.beginRename()
	m.input.text, m.input.cursor = "label", 2
	m.key(keyMsg(tea.KeyRight, ""))
	require.NotNil(t, m.input)
	require.EqualValues(t, 3, m.input.cursor)
	require.EqualValues(t, "Sandboxes", m.page)
	m.key(keyMsg(tea.KeyLeft, ""))
	require.NotNil(t, m.input)
	require.EqualValues(t, 2, m.input.cursor)
	require.EqualValues(t, "a", m.expanded)
}
