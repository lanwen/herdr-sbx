package panel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestStatusManagementSpaceAndLiveHint(t *testing.T) {
	m := modelFixture(t)
	m.openTab("Status")
	m.worker.State, m.worker.Detail = "running", "Following sandbox lifecycle events."
	if hint, _ := m.contextHint(); hint != m.worker.Detail {
		t.Fatalf("live hint=%q", hint)
	}
	m.pending["worker"] = true
	m.workerEnabled = true
	if !strings.Contains(ansi.Strip(m.renderHelp(110, true)), "disable Management") {
		t.Fatal("pending Management reversal must remain advertised")
	}
	m.key(keyMsg(tea.KeySpace, " "))
	if m.workerEnabled {
		t.Fatal("Space did not reverse Management intent")
	}
	m.stopRequestedAt = time.Now()
	if hint, _ := m.contextHint(); !strings.Contains(hint, "Cancelling") {
		t.Fatalf("cancellation hint=%q", hint)
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
	if m.notice != "" || strings.Contains(hint, "Following") || !strings.Contains(hint, "stopped") {
		t.Fatalf("notice=%q hint=%q", m.notice, hint)
	}
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
		if !strings.Contains(text.String(), want) {
			t.Fatalf("missing %q:\n%s", want, text.String())
		}
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
	if len(rows) != 2 || rows[1].Detail != "Running · Inventory readable" || rows[0].Level != precheck.Ready {
		t.Fatalf("grouped checks: %#v", rows)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	focused := m.statusLines()[0].text
	m.button = 1
	unfocused := m.statusLines()[0].text
	if colorsEnabled() && focused == unfocused {
		t.Fatal("Management focus must brighten its outline")
	}
	t.Setenv("NO_COLOR", "1")
	m.button = 0
	if !strings.Contains(m.statusLines()[0].text, "Management ›") {
		t.Fatal("Management focus must remain visible without color")
	}
	for _, l := range m.statusLines() {
		if strings.Contains(l.text, "\x1b[") {
			t.Fatal("NO_COLOR status contains styling")
		}
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
		if !strings.HasSuffix(ansi.Strip(top), " Running ╮") || !strings.Contains(ansi.Strip(body), "ON | OFF") || !strings.Contains(ansi.Strip(bottom), "last at "+m.worker.LastSync.Local().Format("15:04")+"*") {
			t.Fatalf("width %d card:\n%s\n%s\n%s", width, top, body, bottom)
		}
		for _, l := range lines[:3] {
			if ansi.StringWidth(l.text) != width-4 {
				t.Fatalf("card width %d: %q", width, l.text)
			}
		}
		m.worker.State = "cancelling"
		m.workerControlError = true
		t.Setenv("NO_COLOR", "1")
		lines = m.statusLines()
		if ansi.StringWidth(lines[0].text) > width-4 {
			t.Fatalf("failure badge overflow: %q", lines[0].text)
		}
		if !strings.Contains(lines[0].text, "Cancelling") || !strings.Contains(lines[0].text, "›") {
			t.Fatalf("narrow cancellation/focus lost: %q", lines[0].text)
		}
	}
}
