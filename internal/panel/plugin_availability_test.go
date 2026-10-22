package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedPluginExplainsAvailabilityInBothTables(t *testing.T) {
	const reason = "This plugin needs a build, and there is no supported Linux release for the sandbox."
	m := modelFixture(t)
	m.width = 60
	m.plugins = []guestplugins.Choice{{ID: "example.plugin", UnsupportedReason: reason}}
	for _, lines := range [][]line{m.pluginLines(), m.detailPluginLines(m.rows[0])} {
		var content strings.Builder
		for _, l := range lines {
			plain := ansi.Strip(l.text)
			content.WriteString(plain)
			require.False(t, ansi.StringWidth(plain) > m.width-4, "plugin content exceeds 60 columns: %q", plain)
		}
		got := content.String()
		require.Contains(t, got, "Unsupported")
		require.Contains(t, got, "supported")
		require.Contains(t, got, "Linux release")
		require.NotContains(t, got, "Configure bundle", "removed bundle control is still visible")
	}
}

func TestPluginExplanationFollowsSelectedChoice(t *testing.T) {
	m := modelFixture(t)
	m.width = 110
	m.plugins = []guestplugins.Choice{
		{ID: "enabled.plugin", Enabled: true, Installed: true},
		{ID: "disabled.plugin", Installed: true},
	}
	content := func() string {
		var lines []string
		for _, l := range m.pluginLines() {
			lines = append(lines, ansi.Strip(l.text))
		}
		return strings.Join(lines, " ")
	}
	m.pluginIndex = 0
	{
		got := content()
		require.Contains(t, got, "The matching herdr plugin version will be copied to enabled sandboxes while Management is ON.")
		require.NotContains(t, got, "Existing copies stay intact")
	}
	m.pluginIndex = 1
	{
		got := content()
		require.Contains(t, got, "This herdr plugin will not be synchronized. Existing copies stay intact.")
		require.NotContains(t, got, "The matching herdr plugin version will be copied")
	}
	m.queries["Plugins"] = "not-a-match"
	{
		got := content()
		require.Contains(t, got, "No herdr plugins match this filter")
		require.NotContains(t, got, "Existing copies stay intact")
	}
	m.plugins = nil
	m.queries["Plugins"] = ""
	{
		got := content()
		require.Contains(t, got, "No herdr plugins available")
		require.NotContains(t, got, "The matching herdr plugin version will be copied")
	}
}
