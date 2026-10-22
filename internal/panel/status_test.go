package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStatusManagementSpaceAndLiveHint(t *testing.T) {
	m := modelFixture(t)
	m.openTab("Status")
	m.worker.State, m.worker.Detail = "running", "Following sandbox lifecycle events."
	{
		hint, _ := m.contextHint()
		require.Equal(t, m.worker.Detail, hint, "live hint=%q", hint)
	}
	m.pending["worker"] = true
	m.workerEnabled = true
	require.Contains(t, ansi.Strip(m.renderHelp(110, true)), "disable Management", "pending Management reversal must remain advertised")
	m.key(keyMsg(tea.KeySpace, " "))
	require.False(t, m.workerEnabled, "Space did not reverse Management intent")
	m.stopRequestedAt = time.Now()
	{
		hint, _ := m.contextHint()
		require.Contains(t, hint, "Cancelling", "cancellation hint=%q", hint)
	}
}

func TestStatusCompletedStopRefreshesLiveFeedback(t *testing.T) {
	m := modelFixture(t)
	m.openTab("Status")
	m.refreshing = true
	m.worker.State, m.worker.Detail = "running", "Following sandbox lifecycle events."
	m.notice, m.noticeTarget = "Management stop requested", "worker"
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, notice: "Management stop requested"})
	hint, _ := m.contextHint()
	require.EqualValues(t, "", m.notice)
	require.NotContains(t, hint, "Following")
	require.Contains(t, hint, "stopped")
}

func TestStatusPrerequisitesRetainIndependentCauses(t *testing.T) {
	checks := []precheck.Check{
		{Name: "sbx", Level: precheck.Ready, Detail: "sbx 0.9.0"},
		{Name: "Sandbox capability", Level: precheck.Blocked, Detail: "requires JSON commands", Action: "Update sbx"},
		{Name: "Sandbox daemon", Level: precheck.Ready, Detail: "running"},
		{Name: "Sandbox inventory", Level: precheck.Unknown, Detail: "Cannot read /CaseSensitive", Action: "Inspect inventory"},
		{Name: "Docker authorization", Level: precheck.Unknown, Detail: "not checked because sandbox inventory failed", Action: "duplicate recovery"},
		{Name: "Registration association", Level: precheck.Blocked, Detail: "Ambiguous association", Action: "Resolve association"},
	}
	m := modelFixture(t)
	m.checks.Checks = checks
	var text strings.Builder
	for _, l := range m.statusLines() {
		text.WriteString(ansi.Strip(l.text) + "\n")
	}
	for _, want := range []string{"Sandbox service", "Docker sign-in", "Not checked", "requires JSON commands", "Update sbx", "Cannot read /CaseSensitive", "Inspect inventory", "Ambiguous association", "Resolve association"} {
		require.Contains(t, text.String(), want, "missing %q:\n%s", want, text.String())
	}
}

func TestStatusPrerequisiteGroupingAndUnstyledResults(t *testing.T) {
	m := modelFixture(t)
	m.openTab("Status")
	m.checks.Checks = []precheck.Check{
		{Name: "sbx", Level: precheck.Ready, Detail: "sbx 0.7.0"},
		{Name: "Sandbox capability", Level: precheck.Ready, Detail: "JSON daemon status and inventory are available"},
		{Name: "Sandbox daemon", Level: precheck.Ready, Detail: "running"},
		{Name: "Sandbox inventory", Level: precheck.Ready, Detail: "local sandbox inventory is readable"},
	}
	rows := statusPrerequisites(m.checks.Checks)
	require.Len(t, rows, 2)
	require.EqualValues(t, "Running · Inventory readable", rows[1].Detail)
	require.False(t, rows[0].Level != precheck.Ready)
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	focused := m.statusLines()[0].text
	m.button = 1
	unfocused := m.statusLines()[0].text
	require.False(t, colorsEnabled() && focused == unfocused, "Management focus must brighten its outline")
	t.Setenv("NO_COLOR", "1")
	m.button = 0
	require.Contains(t, m.statusLines()[0].text, "Management ›", "Management focus must remain visible without color")
	for _, l := range m.statusLines() {
		require.NotContains(t, l.text, "\x1b[", "NO_COLOR status contains styling")
	}
}

func TestManagementBordersSeparateStateAndObservationActivity(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		m := modelFixture(t)
		m.width = width
		m.openTab("Status")
		m.worker.State = "running"
		m.worker.LastSync = time.Now().Add(-time.Minute)
		m.refreshing = true
		lines := m.statusLines()
		top, body, bottom := lines[0].text, lines[1].text, lines[2].text
		require.True(t, strings.HasSuffix(ansi.Strip(top), " Running ╮"))
		require.Contains(t, ansi.Strip(body), "ON | OFF")
		require.Contains(t, ansi.Strip(bottom), "last at "+m.worker.LastSync.Local().Format("15:04")+"*")
		for _, l := range lines[:3] {
			require.Equal(t, width-4, ansi.StringWidth(l.text), "card width %d: %q", width, l.text)
		}
		m.worker.State = "cancelling"
		m.workerControlError = true
		t.Setenv("NO_COLOR", "1")
		lines = m.statusLines()
		require.False(t, ansi.StringWidth(lines[0].text) > width-4, "failure badge overflow: %q", lines[0].text)
		require.Contains(t, lines[0].text, "Cancelling")
		require.Contains(t, lines[0].text, "›")
	}
}
