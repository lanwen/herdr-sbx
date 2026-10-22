package panel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
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
			if ansi.StringWidth(plain) > m.width-4 {
				t.Fatalf("plugin content exceeds 60 columns: %q", plain)
			}
		}
		got := content.String()
		if !strings.Contains(got, "Unsupported") || !strings.Contains(got, "supported") || !strings.Contains(got, "Linux release") {
			t.Fatalf("unsupported plugin lacks status or reason: %q", got)
		}
		if strings.Contains(got, "Configure bundle") {
			t.Fatal("removed bundle control is still visible")
		}
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
	if got := content(); !strings.Contains(got, "The matching herdr plugin version will be copied to enabled sandboxes while Management is ON.") || strings.Contains(got, "Existing copies stay intact") {
		t.Fatalf("enabled choice explanation: %q", got)
	}
	m.pluginIndex = 1
	if got := content(); !strings.Contains(got, "This herdr plugin will not be synchronized. Existing copies stay intact.") || strings.Contains(got, "The matching herdr plugin version will be copied") {
		t.Fatalf("disabled choice explanation: %q", got)
	}
	m.queries["Plugins"] = "not-a-match"
	if got := content(); !strings.Contains(got, "No herdr plugins match this filter") || strings.Contains(got, "Existing copies stay intact") {
		t.Fatalf("filtered plugin explanation: %q", got)
	}
	m.plugins = nil
	m.queries["Plugins"] = ""
	if got := content(); !strings.Contains(got, "No herdr plugins available") || strings.Contains(got, "The matching herdr plugin version will be copied") {
		t.Fatalf("empty inventory explanation: %q", got)
	}
}
