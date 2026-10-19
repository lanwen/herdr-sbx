package panel

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestSandboxChoiceShowsRequestedValueWhileSavingAndRevertsOnFailure(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.focus = "plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	m.settings = store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"test.plugin": {Enabled: false}}}
	command := m.changeOverride(false)
	if command == nil {
		t.Fatal("choice was not scheduled")
	}
	choice, loading := m.pluginChoiceDisplay("scope", "a", "test.plugin")
	if choice != "On" || !loading {
		t.Fatalf("choice=%q loading=%v", choice, loading)
	}
	if rendered := strings.Join(lineTexts(m.detailPluginLines(m.rows[0])), "\n"); !strings.Contains(rendered, "Loading") {
		t.Fatal("effective column has no loader")
	}
	change := m.pendingChoices["a"]
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodePluginChoice, err: errors.New("write failed"), pluginChoice: &change})
	choice, loading = m.pluginChoiceDisplay("scope", "a", "test.plugin")
	if choice != "Inherit" || loading || len(m.errors) == 0 {
		t.Fatalf("failed choice did not revert: %q loading=%v errors=%v", choice, loading, m.errors)
	}
}

func TestPluginDefaultShowsRequestedValueWhileSaving(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	command := m.toggleDefault()
	if command == nil || !m.visiblePlugins()[0].Enabled {
		t.Fatal("default did not update immediately")
	}
	if m.toggleDefault() != nil {
		t.Fatal("pending default was not gated")
	}
	message := command().(operationMsg)
	if message.err != nil {
		t.Fatal(message.err)
	}
	m.refreshing = true
	m.Update(message)
	if !m.plugins[0].Enabled || !m.settings.Plugins["test.plugin"].Enabled {
		t.Fatal("saved default did not remain visible")
	}
}

func TestPluginDefaultRevertsOnSaveFailure(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	if m.toggleDefault() == nil || !m.visiblePlugins()[0].Enabled {
		t.Fatal("requested default was not visible")
	}
	change := m.pendingChoices["plugin:test.plugin"]
	m.refreshing = true
	m.Update(operationMsg{target: "plugin:test.plugin", code: store.CodePluginChoice, err: errors.New("write failed"), pluginChoice: &change})
	if m.visiblePlugins()[0].Enabled || len(m.errors) == 0 {
		t.Fatal("failed default did not revert and show an error")
	}
}

func lineTexts(lines []line) []string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	return texts
}

func TestSandboxPluginChoiceCyclesIndependentOfDefault(t *testing.T) {
	for _, defaultOn := range []bool{false, true} {
		name := "default off"
		if defaultOn {
			name = "default on"
		}
		t.Run(name, func(t *testing.T) {
			m := modelFixture(t)
			m.expanded = "a"
			m.focus = "plugins"
			m.plugins = []guestplugins.Choice{{ID: "test.plugin", Enabled: defaultOn}}
			if err := store.SetGuestPluginDefault("test.plugin", defaultOn); err != nil {
				t.Fatal(err)
			}
			m.settings, _ = store.ReadGuestPluginDefaults()
			for step := 0; step < 3; step++ {
				var command tea.Cmd
				switch step {
				case 0:
					command = m.key(keyMsg(tea.KeyEnter, ""))
				case 1:
					command = m.key(keyMsg(tea.KeySpace, " "))
				case 2:
					command = m.hitAction(hit{action: "override", id: "test.plugin"})
				}
				if command == nil || m.dialog != "" {
					t.Fatal("choice did not schedule directly")
				}
				if m.changeOverride(false) != nil {
					t.Fatal("pending choice was not gated")
				}
				message := command().(operationMsg)
				if message.err != nil {
					t.Fatal(message.err)
				}
				delete(m.pending, "a")
				m.settings, _ = store.ReadGuestPluginDefaults()
				value, exists := m.settings.Overrides[store.StatusKey("scope", "a")]["test.plugin"]
				if exists != (step < 2) || (step < 2 && value != (step == 0)) {
					t.Fatalf("step%d value=%v exists=%v", step, value, exists)
				}
			}
			if effective := m.settings.Effective("scope", "a").Plugins["test.plugin"].Enabled; effective != defaultOn {
				t.Fatal("Inherit did not restore live default")
			}
		})
	}
}
func TestPluginDefaultsToggleDirectly(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	for step := 0; step < 3; step++ {
		var command tea.Cmd
		switch step {
		case 0:
			command = m.key(keyMsg(tea.KeyEnter, ""))
		case 1:
			command = m.key(keyMsg(tea.KeySpace, " "))
		case 2:
			command = m.hitAction(hit{action: "plugin", id: "test.plugin"})
		}
		if command == nil || m.dialog != "" {
			t.Fatal("default toggle opened a dialog")
		}
		if message := command().(operationMsg); message.err != nil {
			t.Fatal(message.err)
		}
		delete(m.pending, "plugin:test.plugin")
		settings, _ := store.ReadGuestPluginDefaults()
		m.plugins[0].Enabled = settings.Plugins["test.plugin"].Enabled
		if m.plugins[0].Enabled != (step%2 == 0) {
			t.Fatal("default toggle was not saved")
		}
	}
}

func TestUnsupportedPluginCannotBeEnabledButCanBeTurnedOff(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", UnsupportedReason: "Linux binary is unavailable."}}
	if m.toggleDefault() != nil || !strings.Contains(m.notice, "Linux binary is unavailable") {
		t.Fatal("unsupported default was enabled or explanation missing")
	}
	m.plugins[0].Enabled = true
	if m.toggleDefault() == nil {
		t.Fatal("could not turn off unsupported default")
	}

	m = modelFixture(t)
	m.expanded = "a"
	m.focus = "plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", UnsupportedReason: "Linux binary is unavailable."}}
	if m.changeOverride(false) != nil || !strings.Contains(m.notice, "Linux binary is unavailable") {
		t.Fatal("unsupported sandbox override was enabled or explanation missing")
	}
	m.settings.Overrides = map[string]map[string]bool{store.StatusKey("scope", "a"): {"test.plugin": true}}
	if m.changeOverride(false) == nil {
		t.Fatal("could not turn off unsupported sandbox override")
	}
}

func TestExpandedControlsTargetExpandedSandboxAfterArrowSelection(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	m.key(keyMsg(tea.KeyEnd, ""))
	if m.selected != "b" || m.expanded != "a" {
		t.Fatal("selection did not retain details")
	}
	command := m.hitAction(hit{action: "override", id: "test.plugin"})
	if command == nil || m.selected != "a" || m.expanded != "a" {
		t.Fatal("plugin control did not retain and target expanded sandbox")
	}
	if result := command().(operationMsg); result.err != nil {
		t.Fatal(result.err)
	}
	settings, _ := store.ReadGuestPluginDefaults()
	if !settings.Overrides[store.StatusKey("scope", "a")]["test.plugin"] || len(settings.Overrides[store.StatusKey("scope", "b")]) != 0 {
		t.Fatal("plugin choice saved against selected rather than expanded sandbox")
	}
	m.selected = "b"
	m.hitAction(hit{action: "action", id: "rename"})
	if m.input == nil || m.input.target != "a" || m.expanded != "a" {
		t.Fatal("rename control targeted the wrong sandbox or closed details")
	}
	m.input = nil
	m.mergeRows(m.rows[1:])
	m.ensureSelection()
	if m.expanded != "" {
		t.Fatal("deleted sandbox expansion survived")
	}
}
