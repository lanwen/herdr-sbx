package panel

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSandboxChoiceShowsRequestedValueWhileSavingAndRevertsOnFailure(t *testing.T) {
	m := modelFixture(t)
	m.expanded = "a"
	m.focus = "plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	m.settings = store.GuestPluginDefaults{Plugins: map[string]store.GuestPluginDefault{"test.plugin": {Enabled: false}}}
	command := m.changeOverride(false)
	require.NotNil(t, command, "choice was not scheduled")
	choice, loading := m.pluginChoiceDisplay("scope", "a", "test.plugin")
	require.EqualValues(t, "On", choice)
	require.True(t, loading)

	rendered := strings.Join(lineTexts(m.detailPluginLines(m.rows[0])), "\n")
	require.Contains(t, rendered, "Loading", "effective column has no loader")

	change := m.pendingChoices["a"]
	m.refreshing = true
	m.Update(operationMsg{target: "a", code: store.CodePluginChoice, err: errors.New("write failed"), pluginChoice: &change})
	choice, loading = m.pluginChoiceDisplay("scope", "a", "test.plugin")
	require.EqualValues(t, "Inherit", choice)
	require.False(t, loading)
	require.NotEqualValues(t, 0, len(m.errors))
}

func TestPluginDefaultShowsRequestedValueWhileSaving(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	command := m.toggleDefault()
	require.NotNil(t, command)
	require.True(t, m.visiblePlugins()[0].Enabled)
	require.Nil(t, m.toggleDefault(), "pending default was not gated")
	message := command().(operationMsg)
	require.NoError(t, message.err)
	m.refreshing = true
	m.Update(message)
	require.True(t, m.plugins[0].Enabled)
	require.True(t, m.settings.Plugins["test.plugin"].Enabled)
}

func TestPluginDefaultRevertsOnSaveFailure(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	require.NotNil(t, m.toggleDefault())
	require.True(t, m.visiblePlugins()[0].Enabled)
	change := m.pendingChoices["plugin:test.plugin"]
	m.refreshing = true
	m.Update(operationMsg{target: "plugin:test.plugin", code: store.CodePluginChoice, err: errors.New("write failed"), pluginChoice: &change})
	require.False(t, m.visiblePlugins()[0].Enabled)
	require.NotEqualValues(t, 0, len(m.errors))
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
			require.NoError(t, store.SetGuestPluginDefault("test.plugin", defaultOn))
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
				require.NotNil(t, command)
				require.EqualValues(t, "", m.dialog)
				require.Nil(t, m.changeOverride(false), "pending choice was not gated")
				message := command().(operationMsg)
				require.NoError(t, message.err)
				delete(m.pending, "a")
				m.settings, _ = store.ReadGuestPluginDefaults()
				value, exists := m.settings.Overrides[store.StatusKey("scope", "a")]["test.plugin"]
				require.False(t, exists != (step < 2))
				require.False(t, step < 2 && value != (step == 0))
			}

			effective := m.settings.Effective("scope", "a").Plugins["test.plugin"].Enabled
			require.Equal(t, defaultOn, effective, "Inherit did not restore live default")
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
		require.NotNil(t, command)
		require.EqualValues(t, "", m.dialog)

		message := command().(operationMsg)
		require.NoError(t, message.err)

		delete(m.pending, "plugin:test.plugin")
		settings, _ := store.ReadGuestPluginDefaults()
		m.plugins[0].Enabled = settings.Plugins["test.plugin"].Enabled
		require.Equal(t, (step%2 == 0), m.plugins[0].Enabled, "default toggle was not saved")
	}
}

func TestUnsupportedPluginCannotBeEnabledButCanBeTurnedOff(t *testing.T) {
	m := modelFixture(t)
	m.page = "Plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", UnsupportedReason: "Linux binary is unavailable."}}
	require.Nil(t, m.toggleDefault())
	require.Contains(t, m.notice, "Linux binary is unavailable")
	m.plugins[0].Enabled = true
	require.NotNil(t, m.toggleDefault(), "could not turn off unsupported default")

	m = modelFixture(t)
	m.expanded = "a"
	m.focus = "plugins"
	m.plugins = []guestplugins.Choice{{ID: "test.plugin", UnsupportedReason: "Linux binary is unavailable."}}
	require.Nil(t, m.changeOverride(false))
	require.Contains(t, m.notice, "Linux binary is unavailable")
	m.settings.Overrides = map[string]map[string]bool{store.StatusKey("scope", "a"): {"test.plugin": true}}
	require.NotNil(t, m.changeOverride(false), "could not turn off unsupported sandbox override")
}

func TestPreviewControlsTargetSandboxAfterArrowSelection(t *testing.T) {
	m := modelFixture(t)
	m.plugins = []guestplugins.Choice{{ID: "test.plugin"}}
	m.key(keyMsg(tea.KeyEnd, ""))
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "table", m.focus)
	command := m.hitAction(hit{action: "override", id: "test.plugin"})
	require.NotNil(t, command)
	require.EqualValues(t, "b", m.selected)
	require.EqualValues(t, "b", m.expanded)
	require.EqualValues(t, "plugins", m.focus)
	result := command().(operationMsg)
	require.NoError(t, result.err)
	m.refreshing = true
	m.Update(result)
	settings, _ := store.ReadGuestPluginDefaults()
	require.True(t, settings.Overrides[store.StatusKey("scope", "b")]["test.plugin"])
	require.Len(t, settings.Overrides[store.StatusKey("scope", "a")], 0)
	m.hitAction(hit{action: "action", id: "rename"})
	require.NotNil(t, m.input)
	require.EqualValues(t, "b", m.input.target)
	require.EqualValues(t, "b", m.expanded)
	m.input = nil
	m.mergeRows(m.rows[:1])
	m.ensureSelection()
	require.EqualValues(t, "a", m.selected)
	require.EqualValues(t, "a", m.expanded)
	require.EqualValues(t, "table", m.focus)
}
