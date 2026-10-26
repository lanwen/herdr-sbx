package panel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestBrowsingKeysCycleTabsAndMouseSelects(t *testing.T) {
	m := modelFixture(t)
	for _, step := range []struct {
		key  rune
		page string
	}{{tea.KeyLeft, "Sandboxes"}, {tea.KeyTab, "Status"}, {tea.KeyRight, "Status"}, {tea.KeyLeft, "Status"}, {tea.KeyTab, "Plugins"}, {tea.KeyTab, "Sandboxes"}} {
		m.key(keyMsg(step.key, ""))
		require.False(t, m.page != step.page)
		require.EqualValues(t, "table", m.focus)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	require.Equal(t, "Plugins", m.page, "Shift+Tab did not reverse")
	m.View()
	for _, h := range m.hits {
		if h.action == "page" && h.id == "Sandboxes" && h.y == 0 {
			clickAt(m, h.x, h.y)
			require.Equal(t, "Sandboxes", m.page, "tab click failed")
			return
		}
	}
	t.Fatal("missing top tab hit")
}
func TestTabCyclingDoesNotEscapeTextOrDialogs(t *testing.T) {
	m := modelFixture(t)
	m.beginRename()
	m.input.text = "abc"
	m.input.cursor = 2
	m.key(keyMsg(tea.KeyLeft, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, 1, m.input.cursor)
	m.key(keyMsg(tea.KeyTab, ""))
	require.Equal(t, "Sandboxes", m.page, "text Tab changed page")
	m.input = nil
	m.dialog = "startup"
	m.button = 0
	m.key(keyMsg(tea.KeyRight, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, 1, m.button)
	m.key(keyMsg(tea.KeyTab, ""))
	require.EqualValues(t, "Sandboxes", m.page)
	require.EqualValues(t, 2, m.button)
}
func TestStatusStartsAndStopsThroughOperationCommands(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	started, stopped := 0, 0
	m.startWorkerFn = func() error { started++; return nil }
	m.stopWorkerFn = func(context.Context) error { stopped++; return nil }
	m.key(keyMsg('b', "b"))
	require.EqualValues(t, "Status", m.page)
	require.EqualValues(t, "", m.dialog)
	command := m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, 0, started)
	require.NotNil(t, command)
	message := command()
	require.Equal(t, 1, started, "start operation not invoked")
	delete(m.pending, "worker")

	result, ok := message.(operationMsg)
	require.True(t, ok)
	require.Nil(t, result.err)

	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, initialized: true})
	m.button = 0
	command = m.key(keyMsg(tea.KeyEnter, ""))
	require.EqualValues(t, 0, stopped)
	require.NotNil(t, command)
	command()
	require.Equal(t, 1, stopped, "stop operation not invoked")
	m.key(keyMsg(tea.KeyEnd, ""))
	{
		action := m.statusActions()[m.button]
		require.Equal(t, "refresh", action.id, "End did not select refresh while Management is off")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	{
		action := m.statusActions()[m.button]
		require.Equal(t, "ssh", action.id, "Up did not select SSH")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	require.Equal(t, 0, m.button, "Up did not select Management")
}
func TestTopTabsAndStatusControlsFitAndReveal(t *testing.T) {
	for _, width := range []int{60, 110} {
		m := styledFixture(t)
		m.width, m.height = width, 18
		m.worker.State = "stopped"
		m.openTab("Status")
		m.key(keyMsg(tea.KeyEnd, ""))
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		require.Len(t, lines, 18)
		require.Contains(t, lines[0], "Status")
		found := false
		for _, h := range m.hits {
			if h.action == "status-action" && h.id == "2" {
				found = true
				require.Contains(t, ansi.Cut(lines[h.y], h.x, h.x+h.w), "Refresh", "status mouse bounds differ from control")
			}
		}
		require.True(t, found, "focused Status action hidden")
		for _, h := range m.hits {
			require.False(t, h.action == "page" && h.id == "Plugins" && h.y != 0, "obsolete bottom navigation")
		}
	}
}

func TestSectionTabsRotateActiveToLeftAndNamesRemainClickable(t *testing.T) {
	for _, width := range []int{30, 60, 110} {
		m := modelFixture(t)
		m.width = width
		for _, page := range []string{"Sandboxes", "Status", "Plugins"} {
			m.openTab(page)
			strip := m.tabStrip()
			header := ansi.Strip(strip.text)
			require.True(t, strings.HasPrefix(header, "╭─ "))
			require.True(t, strings.HasSuffix(header, "╮"))
			require.False(t, ansi.StringWidth(header) != width)
			require.True(t, strings.HasPrefix(strings.TrimPrefix(header, "╭─ "), func() string {
				if width >= 45 {
					return "  " + page + "  "
				}
				return page
			}()), "active tab not leftmost: %q", header)
			require.False(t, strings.ContainsAny(header, "←→"), "tab strip suggests arrow keys: %q", header)
			count := 0
			for _, h := range strip.hits {
				require.Equal(t, "page", h.action, "non-tab control remains: %#v", h)
				require.Contains(t, ansi.Cut(header, h.x, h.x+h.w), h.id, "tab hit does not cover its name")
				m.hitAction(h)
				require.Equal(t, h.id, m.page, "tab name click did not select tab")
				count++
			}
			require.False(t, width < 32 && count != 1)
			require.False(t, width >= 32 && count != 3)
			m.openTab(page)
			m.key(keyMsg(tea.KeyTab, ""))
			require.NotEqual(t, page, m.page, "Tab cannot reach hidden pages")
			m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			require.Equal(t, page, m.page, "Shift+Tab did not return")
		}
	}
}

func TestWideHelpShowsTabNavigation(t *testing.T) {
	m := modelFixture(t)

	got := ansi.Strip(m.renderHelp(110, false))
	require.Contains(t, got, "tab/shift+tab tabs", got)
}

func TestTabsUseTextColorsWithoutFill(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.page = "Status"
	header := m.tabStrip().text
	require.NotContains(t, header, "\x1b[48;")
	require.NotContains(t, ansi.Strip(header), "[")
	require.NotContains(t, ansi.Strip(header), "]")
	require.Contains(t, header, colorAccent+"  Status  ")
	require.Contains(t, header, colorMuted+"  Plugins  ")
	require.Contains(t, header, colorMuted+"  Sandboxes  ")
}

func TestManagementChoiceAndObservedStateStaySeparate(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	m.workerLoaded = true
	m.startWorkerFn = func() error { return nil }
	command := m.setWorkerEnabled(true)
	require.NotNil(t, command)
	require.Nil(t, m.setWorkerEnabled(true))
	require.EqualValues(t, "stopped", m.worker.State)
	require.Contains(t, ansi.Strip(m.statusFooter().text), "Management ON", "footer hid requested ON")
	for _, state := range []string{"running", "reconnecting", "stopped"} {
		m.Update(workerMsg{status: store.WorkerStatus{State: state}, initialized: true})
		require.True(t, m.workerEnabled)
		require.Contains(t, ansi.Strip(m.statusFooter().text), "Management ON")
	}
}

func TestStatusShowsWorkerSyncTimeOnlyWhileRunning(t *testing.T) {
	m := modelFixture(t)
	m.worker.LastSync = time.Now().Add(-31 * time.Second)
	stamp := m.worker.LastSync.Local().Format("15:04")
	for _, state := range []string{"running", "starting", "stopped", "reconnecting", "error"} {
		m.worker.State = state
		m.refreshing = true
		text := ansi.Strip(m.managementStatus())
		require.Equal(t, (state == "running"), strings.Contains(text, "last at"), "timestamp state %s: %s", state, text)
		require.False(t, state == "running" && !strings.Contains(text, "last at "+stamp+"*"), text)
		require.NotContains(t, ansi.Strip(m.statusFooter().text), "last at", "footer must stay compact")
	}
	m.worker.State = "running"
	m.refreshing = false
	{
		text := ansi.Strip(m.managementStatus())
		require.Contains(t, text, "last at "+stamp)
		require.NotContains(t, text, "*")
	}
	m.checksPending = true
	require.Contains(t, ansi.Strip(m.managementStatus()), stamp+"*", "pending prerequisite refresh needs marker")
	m.worker.LastSync = time.Now().Add(-29 * time.Second)
	{
		text := ansi.Strip(m.managementStatus())
		require.NotContains(t, text, "last at")
		require.Contains(t, text, "*")
	}
	m.checksPending = false
	{
		text := ansi.Strip(m.managementStatus())
		require.NotContains(t, text, "last at")
		require.NotContains(t, text, "*")
	}
	m.checksPending = true
	m.worker.LastSync = time.Time{}
	{
		text := ansi.Strip(m.managementStatus())
		require.NotContains(t, text, "last at")
		require.Contains(t, text, "*")
	}
}

func TestStatusChoicesKeepIntentDistinctFromObservedWorker(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.workerEnabled = true
	m.Update(workerMsg{status: store.WorkerStatus{State: "reconnecting"}, control: &store.WorkerControl{Initialized: true}, initialized: true})
	require.True(t, m.workerEnabled, "reconnect must retain desired ON setting")
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	require.True(t, m.workerEnabled, "poll changed the ON choice to OFF")
	m.workerEnabled = false
	m.startWorkerFn = func() error { return nil }
	m.openTab("Status")
	command := m.activate()
	require.NotNil(t, command)
	require.True(t, m.workerEnabled)
	require.EqualValues(t, "stopped", m.worker.State)
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	require.True(t, m.workerEnabled, "poll must not overwrite requested choice during operation")
	delete(m.pending, "worker")
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	require.True(t, m.workerEnabled, "poll changed the selected ON choice")
	for _, width := range []int{60, 110} {
		m.width = width
		view := m.View()
		require.Contains(t, ansi.Strip(view.Content), "ON | OFF", "status must show both choices")
		found := map[string]bool{}
		for _, h := range m.hits {
			if h.action == "status-choice" {
				found[h.id] = true
			}
		}
		require.True(t, found["on"])
		require.True(t, found["off"])
	}
}

func TestStatusRefreshAvailableUnlessAutoSyncIsHealthyAndRecent(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		enabled bool
		state   string
		last    time.Time
		errors  bool
		want    bool
	}{
		{"recent", true, "running", now.Add(-29 * time.Second), false, false},
		{"at threshold", true, "running", now.Add(-30 * time.Second), false, false},
		{"stale", true, "running", now.Add(-30*time.Second - time.Nanosecond), false, true},
		{"unknown sync time", true, "running", time.Time{}, false, true},
		{"off but running", false, "running", now, false, true},
		{"starting", true, "starting", now, false, true},
		{"pending", true, "pending", now, false, true},
		{"stopped", true, "stopped", now, false, true},
		{"reconnecting", true, "reconnecting", now, false, true},
		{"daemon error", true, "error", now, false, true},
		{"active error", true, "running", now, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := modelFixture(t)
			m.workerEnabled = test.enabled
			m.worker.State, m.worker.LastSync = test.state, test.last
			if test.errors {
				m.errors = []uiError{{code: store.CodeGuestPluginSync, scope: "a", detail: "plugin failed"}}
			}
			found := false
			for _, action := range m.statusActionsAt(now) {
				if action.id == "refresh" {
					found = true
				}
			}
			require.Equal(t, test.want, found, "refresh available=%v, want %v", found, test.want)
		})
	}
}

func TestStatusOffersSSHSetupUntilReady(t *testing.T) {
	for _, level := range []precheck.Level{precheck.Unknown, precheck.Blocked, precheck.Ready} {
		m := modelFixture(t)
		m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: level}}
		m.openTab("Status")
		actions := m.statusActions()
		found := false
		for i, action := range actions {
			if action.id == "ssh" {
				found = true
				m.button = i
				m.activate()
				require.Equal(t, "ssh", m.dialog, "SSH action did not open setup")
			}
		}
		require.Equal(t, (level != precheck.Ready), found, "SSH setup offered=%v for %s", found, level)
	}
}

func TestStatusUsesStoppedDetailAsHint(t *testing.T) {
	for _, detail := range []string{"Daemon is not running.", "Stopped after losing the herdr connection."} {
		m := modelFixture(t)
		m.openTab("Status")
		m.worker.State, m.worker.Detail = "stopped", detail
		hint, _ := m.contextHint()
		require.False(t, hint != detail)
		require.False(t, m.worker.Detail != detail)
	}
}

func TestStatusRefreshFollowsPrerequisitesWithBlankLine(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready, Detail: "SSH configured"}}
	m.openTab("Status")
	m.key(keyMsg(tea.KeyEnd, ""))
	lines := m.statusLines()
	require.False(t, len(lines) < 3)
	require.EqualValues(t, "", lines[len(lines)-2].text)
	require.Contains(t, ansi.Strip(lines[len(lines)-3].text), "SSH configured")
	require.Contains(t, ansi.Strip(lines[len(lines)-1].text), "Refresh")
	last := lines[len(lines)-1]
	require.Len(t, last.hits, 1)
	require.EqualValues(t, "1", last.hits[0].id)
	require.EqualValues(t, "refresh", m.statusActions()[m.button].id)
	m.View()
	for _, hit := range m.hits {
		if hit.action == "status-action" && hit.id == last.hits[0].id {
			m.button = 0
			command := clickAt(m, hit.x, hit.y)
			require.NotNil(t, command)
			require.EqualValues(t, "refresh", m.statusActions()[m.button].id)
			return
		}
	}
	t.Fatal("refresh has no visible mouse control")
}

func TestStatusMouseToggleRequestsDesiredState(t *testing.T) {
	for _, choice := range []struct {
		state string
		want  bool
	}{{"stopped", true}, {"running", false}} {
		m := modelFixture(t)
		m.worker.State = choice.state
		m.workerEnabled = !choice.want
		m.startWorkerFn = func() error { return nil }
		m.stopWorkerFn = func(context.Context) error { return nil }
		m.openTab("Status")
		m.View()
		found := false
		for _, h := range m.hits {
			if h.action == "status-choice" && h.id == map[bool]string{true: "on", false: "off"}[choice.want] {
				command := clickAt(m, h.x, h.y)
				require.NotNil(t, command, "choice click did not request operation")

				found = true
				break
			}
		}
		require.True(t, found)
		require.False(t, m.workerEnabled != choice.want)
		require.False(t, m.worker.State != choice.state)
	}
}

func TestDaemonStopTimeoutHasSpecificReadableError(t *testing.T) {
	m := modelFixture(t)
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, err: context.DeadlineExceeded})
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].code != store.CodeDaemonUnreachable)
	require.Contains(t, m.errors[0].detail, "Cannot reach daemon")
}

func TestOldWorkerPollCannotTurnSavedOffChoiceBackOn(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.worker.State = "running"
	m.workerEnabled = true
	require.NoError(t, store.SetWorkerControl(false))
	stale := m.pollWorker()().(workerMsg)
	m.stopWorkerFn = func(context.Context) error { return store.SetWorkerControl(true) }
	command := m.setWorkerEnabled(false)
	require.NotNil(t, command, "OFF did not save")
	m.Update(command())
	require.False(t, m.workerEnabled, "OFF was not selected after saving")
	m.Update(stale)
	require.False(t, m.workerEnabled, "older status poll changed saved OFF back to ON")
	fresh := m.pollWorker()().(workerMsg)
	m.Update(fresh)
	require.False(t, m.workerEnabled, "fresh status poll did not preserve saved OFF")
}

func TestWorkerPollOnlyLoadsChoiceOnce(t *testing.T) {
	modelFixture(t)
	require.NoError(t, store.SetWorkerControl(false))
	m := newModel(nil)
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true}, initialized: true})
	require.True(t, m.workerEnabled, "saved ON choice was not loaded when the panel opened")
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true, Stopped: true}, initialized: true})
	require.True(t, m.workerEnabled, "status poll changed the ON choice")
	require.NoError(t, store.SetWorkerControl(true))
	require.NoError(t, store.RecordError(store.CodeDaemonStop, "worker", "cannot save stop request"))
	m = newModel(nil)
	require.False(t, m.workerEnabled)
	require.True(t, m.workerControlError)
}

func TestFailedOffKeepsChoiceAndObservedRunning(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.workerLoaded = true
	m.workerEnabled = true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return errors.New("cannot save stop request") }
	command := m.setWorkerEnabled(false)
	require.NotNil(t, command)
	require.False(t, m.workerEnabled)
	require.Contains(t, ansi.Strip(m.managementStatus()), "Cancelling")
	m.Update(command())
	line := ansi.Strip(m.statusLines()[0].text)
	require.False(t, m.workerEnabled)
	require.Contains(t, line, "ERROR")
	require.Contains(t, line, "Running")
	require.Contains(t, m.statusLines()[1].text, ink(colorError, "OFF"), "failed OFF choice is not red")
	require.Contains(t, ansi.Strip(m.View().Content), store.CodeDaemonStop+" · worker", "stop failure missing from Errors section")
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true}, initialized: true, observedAt: time.Now()})
	require.False(t, m.workerEnabled, "status poll changed failed OFF back to ON")
	m.stopWorkerFn = func(context.Context) error { return nil }
	m.button = 0

	retry := m.statusAction()
	require.NotNil(t, retry)
	require.Contains(t, ansi.Strip(m.managementStatus()), "Cancelling")
}

func TestStopRequestReportsLongRunningDaemon(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "running"
	m.workerEnabled = false
	m.stopRequestedAt = time.Now().Add(-time.Minute - time.Second)
	m.Update(tickMsg(time.Now()))
	require.Len(t, m.errors, 1)
	require.False(t, m.errors[0].code != store.CodeDaemonUnreachable)
	m.Update(tickMsg(time.Now()))
	require.Len(t, m.errors, 1)
	require.EqualValues(t, 1, m.errors[0].count)
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	require.Len(t, m.errors, 0, "stop did not clear delayed-stop error: %+v", m.errors)
}
