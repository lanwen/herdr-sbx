package panel

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestPhoneFramesFitAndControlsRemainReachable(t *testing.T) {
	for _, plain := range []bool{false, true} {
		for _, width := range []int{30, 31, 32, 36, 40, 44, 45, 59, 60, 71, 72, 110} {
			t.Run(fmt.Sprintf("%d/plain=%v", width, plain), func(t *testing.T) {
				m := modelFixture(t)
				if plain {
					t.Setenv("NO_COLOR", "")
				} else {
					unsetNoColor(t)
				}
				m.width, m.height = width, 12
				m.rows[0].Status.Detail = strings.Repeat("Long status explanation. ", 20)
				m.plugins = []guestplugins.Choice{{ID: "example.very-long-plugin-name"}}
				check := func() {
					t.Helper()
					content := ansi.Strip(m.View().Content)
					lines := strings.Split(content, "\n")
					if len(lines) != m.height {
						t.Fatalf("height %d want %d: %s", len(lines), m.height, content)
					}
					for _, line := range lines {
						if ansi.StringWidth(line) > width {
							t.Fatalf("overflow: %q", line)
						}
					}
					for _, h := range m.hits {
						if h.x < 0 || h.w < 1 || h.x+h.w > width || h.y < 0 || h.y >= m.height {
							t.Fatalf("bad hit: %+v", h)
						}
					}
					if strings.Contains(content, "Resize") {
						t.Fatal("phone was rejected")
					}
				}
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
					found := false
					for _, h := range m.hits {
						if h.action == "action" && h.id == action.id {
							found = true
						}
					}
					if !found {
						t.Fatalf("action %s unreachable", action.id)
					}
				}
				m.focus = "plugins"
				m.detailIndex = 0
				m.reveal = true
				check()
				found := false
				for _, h := range m.hits {
					if h.action == "override" {
						found = true
					}
				}
				if !found {
					t.Fatal("plugin choice unreachable")
				}
			})
		}
	}
}

func TestPhoneTablePreservesBothNamesAndManagement(t *testing.T) {
	for _, width := range []int{30, 32, 36, 40, 59, 60, 71, 72, 110} {
		m := modelFixture(t)
		m.width = width
		c := m.sandboxColumns(m.rows)
		if got := ansi.StringWidth(c.header()); got != width-4 {
			t.Fatalf("width %d table %d", width, got)
		}
		if (c.readiness == 0) != (width < 72) {
			t.Fatalf("readiness at %d: %+v", width, c)
		}
		if c.name < 1 || c.label < 1 {
			t.Fatalf("names lost: %+v", c)
		}
		if width < 60 {
			if !c.compactRuntime {
				t.Fatal("runtime did not compact")
			}
			for choice, want := range map[store.ManagementChoice]string{store.Watched: "W", store.Unmanaged: "UM", store.Unwatched: "UW", store.Disabled: "D", store.Untracked: "UT", "other": "?"} {
				if got := c.managementText(choice); got != want {
					t.Fatalf("%s=%s", choice, got)
				}
			}
		}
	}
}

func TestOverflowHintRemainsReadableByKeyboardScrolling(t *testing.T) {
	for _, size := range [][2]int{{32, 12}, {60, 18}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = size[0], size[1]
			m.rows[0].Status.Detail = strings.Repeat("A full cause with recovery guidance. ", 20) + "END_RECOVERY."
			var seen strings.Builder
			for i := 0; i < 100; i++ {
				frame := ansi.Strip(m.View().Content)
				seen.WriteString(frame)
				if len(strings.Split(frame, "\n")) != m.height {
					t.Fatal("hint overflowed terminal")
				}
				if !m.hintInBody {
					t.Fatal("long hint was not in viewport")
				}
				m.key(keyMsg(tea.KeyPgDown, ""))
			}
			if !strings.Contains(seen.String(), "END_RECOVERY.") {
				t.Fatal("long hint ending was lost")
			}
			if m.selected != "a" {
				t.Fatal("page scrolling changed sandbox selection")
			}
			if !strings.Contains(seen.String(), "alpha") {
				t.Fatal("hint hid table")
			}
			m.rows[0].Status.Detail = "Short hint"
			m.View()
			if m.hintInBody {
				t.Fatal("stale hint scroll state")
			}
		})
	}
}

func TestNarrowDiagnosticsSwitchRemainsVisibleAndClickable(t *testing.T) {
	for _, size := range [][2]int{{32, 12}, {40, 12}, {60, 18}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := modelFixture(t)
			m.width, m.height = size[0], size[1]
			m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "new failure", count: 2}, {code: store.CodeOwnershipConflict, scope: "b", detail: "dismissed warning", count: 3, dismissed: true}}
			m.focus, m.reveal = "diagnostics", true
			frame := ansi.Strip(m.View().Content)
			if !strings.Contains(frame, "New (2) | All (5)") {
				t.Fatalf("selector clipped: %s", frame)
			}
			selected := false
			lines := strings.Split(frame, "\n")
			for _, h := range m.hits {
				if h.action == "diagnostic-toggle" && strings.Contains(ansi.Cut(lines[h.y], h.x, h.x+h.w), "New (2) | All (5)") {
					m.click(h.x, h.y)
					selected = true
					break
				}
			}
			if !selected || !m.showAllErrors {
				t.Fatal("selector mouse hit missing")
			}
			m.key(keyMsg(tea.KeyLeft, ""))
			if m.showAllErrors {
				t.Fatal("left did not select New")
			}
			m.key(keyMsg(tea.KeyRight, ""))
			if !m.showAllErrors {
				t.Fatal("right did not select All")
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "New (2) | All (5)") {
				t.Fatal("selector lost after keyboard switch")
			}
		})
	}
}

func TestNarrowPluginHintIdentifiesSamePrefixPlugins(t *testing.T) {
	for _, width := range []int{30, 32} {
		for _, page := range []string{"Plugins", "Sandboxes"} {
			t.Run(fmt.Sprintf("%d/%s", width, page), func(t *testing.T) {
				m := modelFixture(t)
				m.width, m.height = width, 12
				m.plugins = []guestplugins.Choice{{ID: "same.plugin.prefix.first"}, {ID: "same.plugin.prefix.second"}}
				m.openTab(page)
				if page == "Sandboxes" {
					m.expanded = "a"
					m.focus = "plugins"
				}
				for index, plugin := range m.plugins {
					if index > 0 {
						m.key(keyMsg(tea.KeyDown, ""))
						if hint, _ := m.contextHint(); !strings.Contains(hint, plugin.ID) {
							t.Fatalf("keyboard focus ID missing: %q", hint)
						}
					}
					m.pluginIndex, m.detailIndex = index, index
					m.pluginSelected = plugin.ID
					hint, _ := m.contextHint()
					if !strings.Contains(hint, plugin.ID) {
						t.Fatalf("focused ID missing: %q", hint)
					}
					found := false
					for attempt := 0; attempt < 30; attempt++ {
						if strings.Contains(ansi.Strip(m.View().Content), plugin.ID) {
							found = true
							break
						}
						if !m.hintInBody {
							break
						}
						m.key(keyMsg(tea.KeyPgDown, ""))
					}
					if !found {
						t.Fatalf("full focused ID not reachable at %d/%s", width, page)
					}
					m.reveal = true
					m.View()
					clicked := false
					for _, h := range m.hits {
						if h.id == plugin.ID && (h.action == "plugin" || h.action == "override") {
							m.click(h.x, h.y)
							clicked = true
							break
						}
					}
					if !clicked {
						t.Fatal("focused plugin missing mouse hit")
					}
					if hint, _ := m.contextHint(); !strings.Contains(hint, plugin.ID) {
						t.Fatalf("mouse focus ID missing: %q", hint)
					}
				}
			})
		}
	}
}
