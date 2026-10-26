package panel

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPhoneFramesFitAndControlsRemainReachable(t *testing.T) {
	for _, plain := range []bool{false, true} {
		for _, width := range []int{30, 31, 32, 36, 40, 44, 45, 59, 60, 71, 72, 110} {
			t.Run(fmt.Sprintf("%d/plain=%v", width, plain), func(t *testing.T) {
				testPhoneFrame(t, width, plain)
			})
		}
	}
}

func TestPhoneTablePreservesEnabledAndNames(t *testing.T) {
	for _, width := range []int{30, 32, 36, 40, 59, 60, 71, 72, 110} {
		m := modelFixture(t)
		m.width = width
		c := m.sandboxColumns(m.rows)
		require.False(t, ansi.StringWidth(c.header()) != width-4)
		require.False(t, (c.status == 0) != (width < 72))
		require.False(t, c.name < 1)
		require.False(t, c.label < 1)
		require.False(t, width < 60 && c.enabled != 1, "enabled symbol column not compact")
	}
}

func TestOverflowHintRemainsReadableByKeyboardScrolling(t *testing.T) {
	for _, size := range [][2]int{{30, 10}, {32, 12}, {60, 18}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = size[0], size[1]
			m.rows[0].Status.Detail = strings.Repeat("A full cause with recovery guidance. ", 20) + "END_RECOVERY."
			var seen strings.Builder
			for i := 0; i < 100; i++ {
				frame := ansi.Strip(m.View().Content)
				seen.WriteString(frame)
				require.Len(t, strings.Split(frame, "\n"), m.height, "hint overflowed terminal")
				require.True(t, m.hintInBody, "long hint was not in viewport")
				m.key(keyMsg(tea.KeyPgDown, ""))
			}
			require.Contains(t, seen.String(), "END_RECOVERY.", "long hint ending was lost")
			require.Equal(t, "a", m.selected, "page scrolling changed sandbox selection")
			require.Contains(t, seen.String(), "alpha", "hint hid table")
			m.rows[0].Status.Detail = "Short hint"
			m.View()
			require.False(t, m.hintInBody, "stale hint scroll state")
		})
	}
}

func TestNarrowDiagnosticsSwitchRemainsVisibleAndClickable(t *testing.T) {
	for _, size := range [][2]int{{30, 10}, {32, 12}, {40, 12}, {60, 18}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = size[0], size[1]
			m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "new failure", count: 2}, {code: store.CodeOwnershipConflict, scope: "b", detail: "dismissed warning", count: 3, dismissed: true}}
			m.focus, m.reveal = "diagnostics", true
			frame := ansi.Strip(m.View().Content)
			require.Contains(t, frame, "New (2) | All (5)", "selector clipped: %s", frame)
			selected := false
			lines := strings.Split(frame, "\n")
			for _, h := range m.hits {
				if h.action == "diagnostic-toggle" && strings.Contains(ansi.Cut(lines[h.y], h.x, h.x+h.w), "New (2) | All (5)") {
					clickAt(m, h.x, h.y)
					selected = true
					break
				}
			}
			require.True(t, selected)
			require.True(t, m.showAllErrors)
			m.key(keyMsg(tea.KeyLeft, ""))
			require.False(t, m.showAllErrors, "left did not select New")
			m.key(keyMsg(tea.KeyRight, ""))
			require.True(t, m.showAllErrors, "right did not select All")
			require.Contains(t, ansi.Strip(m.View().Content), "New (2) | All (5)", "selector lost after keyboard switch")
		})
	}
}

func TestNarrowPluginHintIdentifiesSamePrefixPlugins(t *testing.T) {
	for _, width := range []int{30, 32} {
		for _, page := range []string{"Plugins", "Sandboxes"} {
			t.Run(fmt.Sprintf("%d/%s", width, page), func(t *testing.T) {
				testNarrowPluginHint(t, width, page)
			})
		}
	}
}

func testPhoneFrame(t *testing.T, width int, plain bool) {
	t.Helper()
	m := modelFixture(t)
	if plain {
		t.Setenv("NO_COLOR", "")
	} else {
		unsetNoColor(t)
	}
	m.width, m.height = width, 10
	m.rows[0].Status.Detail = strings.Repeat("Long status explanation. ", 20)
	m.plugins = []guestplugins.Choice{{ID: "example.very-long-plugin-name"}}
	check := func() { assertPhoneFrame(t, m, width) }

	check()
	for _, page := range []string{"Status", "Plugins", "Sandboxes"} {
		m.openTab(page)
		check()
	}
	m.key(keyMsg(tea.KeyEnter, ""))
	m.focus = "actions"
	for i, action := range m.sandboxActions(m.rows[0]) {
		m.button = i
		m.reveal = true
		check()
		_, found := matchingPanelHit(m.hits, func(h hit) bool { return h.action == "action" && h.id == action.id })
		require.True(t, found, "action %s unreachable", action.id)
	}
	m.focus = "plugins"
	m.detailIndex = 0
	m.reveal = true
	check()
	_, found := matchingPanelHit(m.hits, func(h hit) bool { return h.action == "override" })
	require.True(t, found, "plugin choice unreachable")

}

func testNarrowPluginHint(t *testing.T, width int, page string) {
	t.Helper()
	m := modelFixture(t)
	m.width, m.height = width, 10
	m.plugins = []guestplugins.Choice{{ID: "same.plugin.prefix.first"}, {ID: "same.plugin.prefix.second"}}
	m.openTab(page)
	if page == "Sandboxes" {
		m.expanded = "a"
		m.focus = "plugins"
	}
	for index, plugin := range m.plugins {
		assertNarrowPluginFocus(t, m, index, plugin.ID)
	}

}

func assertPhoneFrame(t *testing.T, m *model, width int) {

	t.Helper()
	content := ansi.Strip(m.View().Content)
	lines := strings.Split(content, "\n")
	require.Len(t, lines, m.height, "height %d want %d: %s", len(lines), m.height, content)
	for _, line := range lines {
		require.False(t, ansi.StringWidth(line) > width, "overflow: %q", line)
	}
	for _, h := range m.hits {
		require.False(t, h.x < 0)
		require.False(t, h.w < 1)
		require.False(t, h.x+h.w > width)
		require.False(t, h.y < 0)
		require.False(t, h.y >= m.height)
	}
	require.NotContains(t, content, "Resize", "phone was rejected")
}

func assertNarrowPluginFocus(t *testing.T, m *model, index int, pluginID string) {
	t.Helper()
	width, page := m.width, m.page

	if index > 0 {
		m.key(keyMsg(tea.KeyDown, ""))
		{
			hint, _ := m.contextHint()
			require.Contains(t, hint, pluginID, "keyboard focus ID missing: %q", hint)
		}
	}
	m.pluginIndex, m.detailIndex = index, index
	m.pluginSelected = pluginID
	hint, _ := m.contextHint()
	require.Contains(t, hint, pluginID, "focused ID missing: %q", hint)
	assertFocusedPluginHintReachable(t, m, width, page, pluginID)
	m.reveal = true
	m.View()
	target, clicked := matchingPanelHit(m.hits, func(h hit) bool { return h.id == pluginID && (h.action == "plugin" || h.action == "override") })
	if clicked {
		clickAt(m, target.x, target.y)
	}
	require.True(t, clicked, "focused plugin missing mouse hit")
	{
		hint, _ := m.contextHint()
		require.Contains(t, hint, pluginID, "mouse focus ID missing: %q", hint)
	}
}

func assertFocusedPluginHintReachable(t *testing.T, m *model, width int, page, pluginID string) {
	t.Helper()
	found := false
	for attempt := 0; attempt < 30; attempt++ {
		if strings.Contains(ansi.Strip(m.View().Content), pluginID) {
			found = true
			break
		}
		if !m.hintInBody {
			break
		}
		m.key(keyMsg(tea.KeyPgDown, ""))
	}
	require.True(t, found, "full focused ID not reachable at %d/%s", width, page)
}

func matchingPanelHit(hits []hit, match func(hit) bool) (hit, bool) {
	for _, h := range hits {
		if match(h) {
			return h, true
		}
	}
	return hit{}, false
}
