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
)

func TestBrowsingKeysCycleTabsAndMouseSelects(t *testing.T) {
	m := modelFixture(t)
	for _, step := range []struct {
		key  rune
		page string
	}{{tea.KeyLeft, "Sandboxes"}, {tea.KeyTab, "Status"}, {tea.KeyRight, "Status"}, {tea.KeyLeft, "Status"}, {tea.KeyTab, "Plugins"}, {tea.KeyTab, "Sandboxes"}} {
		m.key(keyMsg(step.key, ""))
		if m.page != step.page || m.focus != "table" {
			t.Fatalf("key %v -> %s/%s", step.key, m.page, m.focus)
		}
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.page != "Plugins" {
		t.Fatal("Shift+Tab did not reverse")
	}
	m.View()
	for _, h := range m.hits {
		if h.action == "page" && h.id == "Sandboxes" && h.y == 0 {
			m.click(h.x, h.y)
			if m.page != "Sandboxes" {
				t.Fatal("tab click failed")
			}
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
	if m.page != "Sandboxes" || m.input.cursor != 1 {
		t.Fatal("text arrow changed tab")
	}
	m.key(keyMsg(tea.KeyTab, ""))
	if m.page != "Sandboxes" {
		t.Fatal("text Tab changed page")
	}
	m.input = nil
	m.dialog = "startup"
	m.button = 0
	m.key(keyMsg(tea.KeyRight, ""))
	if m.page != "Sandboxes" || m.button != 1 {
		t.Fatal("dialog arrow changed tab")
	}
	m.key(keyMsg(tea.KeyTab, ""))
	if m.page != "Sandboxes" || m.button != 2 {
		t.Fatal("dialog Tab changed page")
	}
}
func TestStatusStartsAndStopsThroughOperationCommands(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	started, stopped := 0, 0
	m.startWorkerFn = func() error { started++; return nil }
	m.stopWorkerFn = func(context.Context) error { stopped++; return nil }
	m.key(keyMsg('b', "b"))
	if m.page != "Status" || m.dialog != "" {
		t.Fatal("Management shortcut must open Status")
	}
	command := m.key(keyMsg(tea.KeyEnter, ""))
	if started != 0 || command == nil {
		t.Fatal("start did not schedule asynchronous work")
	}
	message := command()
	if started != 1 {
		t.Fatal("start operation not invoked")
	}
	delete(m.pending, "worker")
	if result, ok := message.(operationMsg); !ok || result.err != nil {
		t.Fatalf("start result %#v", message)
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, initialized: true})
	m.button = 0
	command = m.key(keyMsg(tea.KeyEnter, ""))
	if stopped != 0 || command == nil {
		t.Fatal("stop did not schedule asynchronous work")
	}
	command()
	if stopped != 1 {
		t.Fatal("stop operation not invoked")
	}
	m.key(keyMsg(tea.KeyEnd, ""))
	if action := m.statusActions()[m.button]; action.id != "refresh" {
		t.Fatal("End did not select refresh while Management is off")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	if action := m.statusActions()[m.button]; action.id != "ssh" {
		t.Fatal("Up did not select SSH")
	}
	m.key(keyMsg(tea.KeyUp, ""))
	if m.button != 0 {
		t.Fatal("Up did not select Management")
	}
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
		if len(lines) != 18 || !strings.Contains(lines[0], "Status") {
			t.Fatalf("wrong tab/height: %s", view)
		}
		found := false
		for _, h := range m.hits {
			if h.action == "status-action" && h.id == "2" {
				found = true
				if !strings.Contains(ansi.Cut(lines[h.y], h.x, h.x+h.w), "Refresh") {
					t.Fatal("status mouse bounds differ from control")
				}
			}
		}
		if !found {
			t.Fatal("focused Status action hidden")
		}
		for _, h := range m.hits {
			if h.action == "page" && h.id == "Plugins" && h.y != 0 {
				t.Fatal("obsolete bottom navigation")
			}
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
			if !strings.HasPrefix(header, "╭─ ") || !strings.HasSuffix(header, "╮") || ansi.StringWidth(header) != width {
				t.Fatalf("invalid border: %q", header)
			}
			if !strings.HasPrefix(strings.TrimPrefix(header, "╭─ "), func() string {
				if width >= 45 {
					return "  " + page + "  "
				}
				return page
			}()) {
				t.Fatalf("active tab not leftmost: %q", header)
			}
			if strings.ContainsAny(header, "←→") {
				t.Fatalf("tab strip suggests arrow keys: %q", header)
			}
			count := 0
			for _, h := range strip.hits {
				if h.action != "page" {
					t.Fatalf("non-tab control remains: %#v", h)
				}
				if !strings.Contains(ansi.Cut(header, h.x, h.x+h.w), h.id) {
					t.Fatal("tab hit does not cover its name")
				}
				m.hitAction(h)
				if m.page != h.id {
					t.Fatal("tab name click did not select tab")
				}
				count++
			}
			if width < 32 && count != 1 || width >= 32 && count != 3 {
				t.Fatal("wrong visible tab count", count)
			}
			m.openTab(page)
			m.key(keyMsg(tea.KeyTab, ""))
			if m.page == page {
				t.Fatal("Tab cannot reach hidden pages")
			}
			m.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.page != page {
				t.Fatal("Shift+Tab did not return")
			}
		}
	}
}

func TestWideHelpShowsTabNavigation(t *testing.T) {
	m := modelFixture(t)
	if got := ansi.Strip(m.renderHelp(110, false)); !strings.Contains(got, "tab/shift+tab tabs") {
		t.Fatal(got)
	}
}

func TestTabsUseTextColorsWithoutFill(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.page = "Status"
	header := m.tabStrip().text
	if strings.Contains(header, "\x1b[48;") || strings.Contains(ansi.Strip(header), "[") || strings.Contains(ansi.Strip(header), "]") {
		t.Fatalf("tabs use fill or brackets: %q", header)
	}
	if !strings.Contains(header, colorAccent+"  Status  ") || !strings.Contains(header, colorMuted+"  Plugins  ") || !strings.Contains(header, colorMuted+"  Sandboxes  ") {
		t.Fatalf("tabs have incorrect colors: %q", header)
	}
}

func TestManagementChoiceAndObservedStateStaySeparate(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	m.workerLoaded = true
	m.startWorkerFn = func() error { return nil }
	command := m.setWorkerEnabled(true)
	if command == nil || m.setWorkerEnabled(true) != nil || m.worker.State != "stopped" {
		t.Fatal("duplicate start or false running observation")
	}
	if !strings.Contains(ansi.Strip(m.statusFooter().text), "Management ON") {
		t.Fatal("footer hid requested ON")
	}
	for _, state := range []string{"running", "reconnecting", "stopped"} {
		m.Update(workerMsg{status: store.WorkerStatus{State: state}, initialized: true})
		if !m.workerEnabled || !strings.Contains(ansi.Strip(m.statusFooter().text), "Management ON") {
			t.Fatal("observation overwrote choice")
		}
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
		if strings.Contains(text, "last at") != (state == "running") {
			t.Fatalf("timestamp state %s: %s", state, text)
		}
		if state == "running" && !strings.Contains(text, "last at "+stamp+"*") {
			t.Fatal(text)
		}
		if strings.Contains(ansi.Strip(m.statusFooter().text), "last at") {
			t.Fatal("footer must stay compact")
		}
	}
	m.worker.State = "running"
	m.refreshing = false
	if text := ansi.Strip(m.managementStatus()); !strings.Contains(text, "last at "+stamp) || strings.Contains(text, "*") {
		t.Fatal(text)
	}
	m.checksPending = true
	if !strings.Contains(ansi.Strip(m.managementStatus()), stamp+"*") {
		t.Fatal("pending prerequisite refresh needs marker")
	}
	m.worker.LastSync = time.Now().Add(-29 * time.Second)
	if text := ansi.Strip(m.managementStatus()); strings.Contains(text, "last at") || !strings.Contains(text, "*") {
		t.Fatalf("recent sync needs only pending marker: %s", text)
	}
	m.checksPending = false
	if text := ansi.Strip(m.managementStatus()); strings.Contains(text, "last at") || strings.Contains(text, "*") {
		t.Fatalf("recent completed sync should be compact: %s", text)
	}
	m.checksPending = true
	m.worker.LastSync = time.Time{}
	if text := ansi.Strip(m.managementStatus()); strings.Contains(text, "last at") || !strings.Contains(text, "*") {
		t.Fatal("unknown sync time must hide time but keep pending marker")
	}
}

func TestStatusChoicesKeepIntentDistinctFromObservedWorker(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.workerEnabled = true
	m.Update(workerMsg{status: store.WorkerStatus{State: "reconnecting"}, control: &store.WorkerControl{Initialized: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("reconnect must retain desired ON setting")
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("poll changed the ON choice to OFF")
	}
	m.workerEnabled = false
	m.startWorkerFn = func() error { return nil }
	m.openTab("Status")
	command := m.activate()
	if command == nil || !m.workerEnabled || m.worker.State != "stopped" {
		t.Fatal("toggle must update intent without claiming worker started")
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("poll must not overwrite requested choice during operation")
	}
	delete(m.pending, "worker")
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("poll changed the selected ON choice")
	}
	for _, width := range []int{60, 110} {
		m.width = width
		view := m.View()
		if !strings.Contains(ansi.Strip(view.Content), "ON | OFF") {
			t.Fatal("status must show both choices")
		}
		found := map[string]bool{}
		for _, h := range m.hits {
			if h.action == "status-choice" {
				found[h.id] = true
			}
		}
		if !found["on"] || !found["off"] {
			t.Fatalf("missing ON/OFF choices at width %d", width)
		}
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
			if found != test.want {
				t.Fatalf("refresh available=%v, want %v", found, test.want)
			}
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
				if m.dialog != "ssh" {
					t.Fatal("SSH action did not open setup")
				}
			}
		}
		if found != (level != precheck.Ready) {
			t.Fatalf("SSH setup offered=%v for %s", found, level)
		}
	}
}

func TestStatusUsesStoppedDetailAsHint(t *testing.T) {
	for _, detail := range []string{"Daemon is not running.", "Stopped after losing the herdr connection."} {
		m := modelFixture(t)
		m.openTab("Status")
		m.worker.State, m.worker.Detail = "stopped", detail
		hint, _ := m.contextHint()
		if hint != detail || m.worker.Detail != detail {
			t.Fatalf("detail=%q hint=%q", detail, hint)
		}
	}
}

func TestStatusRefreshFollowsPrerequisitesWithBlankLine(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "stopped"
	m.checks.Checks = []precheck.Check{{Name: "Sandbox SSH", Level: precheck.Ready, Detail: "SSH configured"}}
	m.openTab("Status")
	m.key(keyMsg(tea.KeyEnd, ""))
	lines := m.statusLines()
	if len(lines) < 3 || lines[len(lines)-2].text != "" || !strings.Contains(ansi.Strip(lines[len(lines)-3].text), "SSH configured") || !strings.Contains(ansi.Strip(lines[len(lines)-1].text), "Refresh") {
		t.Fatalf("refresh must follow prerequisite content and a blank line: %#v", lines)
	}
	last := lines[len(lines)-1]
	if len(last.hits) != 1 || last.hits[0].id != "1" || m.statusActions()[m.button].id != "refresh" {
		t.Fatal("refresh mouse or keyboard action differs from visual order")
	}
	m.View()
	for _, hit := range m.hits {
		if hit.action == "status-action" && hit.id == last.hits[0].id {
			m.button = 0
			command := m.click(hit.x, hit.y)
			if command == nil || m.statusActions()[m.button].id != "refresh" {
				t.Fatal("refresh click did not schedule observations")
			}
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
				if command := m.click(h.x, h.y); command == nil {
					t.Fatal("choice click did not request operation")
				}
				found = true
				break
			}
		}
		if !found || m.workerEnabled != choice.want || m.worker.State != choice.state {
			t.Fatalf("mouse toggle lost requested or observed state: %+v", choice)
		}
	}
}

func TestDaemonStopTimeoutHasSpecificReadableError(t *testing.T) {
	m := modelFixture(t)
	m.Update(operationMsg{target: "worker", code: store.CodeDaemonStop, err: context.DeadlineExceeded})
	if len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonUnreachable || !strings.Contains(m.errors[0].detail, "Cannot reach daemon") {
		t.Fatalf("unexpected daemon timeout error: %+v", m.errors)
	}
}

func TestOldWorkerPollCannotTurnSavedOffChoiceBackOn(t *testing.T) {
	m := modelFixture(t)
	m.workerLoaded = true
	m.worker.State = "running"
	m.workerEnabled = true
	if err := store.SetWorkerControl(false); err != nil {
		t.Fatal(err)
	}
	stale := m.pollWorker()().(workerMsg)
	m.stopWorkerFn = func(context.Context) error { return store.SetWorkerControl(true) }
	command := m.setWorkerEnabled(false)
	if command == nil {
		t.Fatal("OFF did not save")
	}
	m.Update(command())
	if m.workerEnabled {
		t.Fatal("OFF was not selected after saving")
	}
	m.Update(stale)
	if m.workerEnabled {
		t.Fatal("older status poll changed saved OFF back to ON")
	}
	fresh := m.pollWorker()().(workerMsg)
	m.Update(fresh)
	if m.workerEnabled {
		t.Fatal("fresh status poll did not preserve saved OFF")
	}
}

func TestWorkerPollOnlyLoadsChoiceOnce(t *testing.T) {
	m := modelFixture(t)
	if err := store.SetWorkerControl(false); err != nil {
		t.Fatal(err)
	}
	m = newModel(nil)
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("saved ON choice was not loaded when the panel opened")
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true, Stopped: true}, initialized: true})
	if !m.workerEnabled {
		t.Fatal("status poll changed the ON choice")
	}
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeDaemonStop, "worker", "cannot save stop request"); err != nil {
		t.Fatal(err)
	}
	m = newModel(nil)
	if m.workerEnabled || !m.workerControlError {
		t.Fatal("saved OFF choice and error were not loaded when the panel opened")
	}
}

func TestFailedOffKeepsChoiceAndObservedRunning(t *testing.T) {
	unsetNoColor(t)
	m := modelFixture(t)
	m.workerLoaded = true
	m.workerEnabled = true
	m.worker.State = "running"
	m.stopWorkerFn = func(context.Context) error { return errors.New("cannot save stop request") }
	command := m.setWorkerEnabled(false)
	if command == nil || m.workerEnabled || !strings.Contains(ansi.Strip(m.managementStatus()), "Cancelling") {
		t.Fatal("OFF did not show the stopping transition")
	}
	m.Update(command())
	line := ansi.Strip(m.statusLines()[0].text)
	if m.workerEnabled || !strings.Contains(line, "ERROR") || !strings.Contains(line, "Running") {
		t.Fatalf("failed OFF lost the choice or runtime: %q", line)
	}
	if !strings.Contains(m.statusLines()[1].text, ink(colorError, "OFF")) {
		t.Fatal("failed OFF choice is not red")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), store.CodeDaemonStop+" · worker") {
		t.Fatal("stop failure missing from Errors section")
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "running"}, control: &store.WorkerControl{Initialized: true}, initialized: true, observedAt: time.Now()})
	if m.workerEnabled {
		t.Fatal("status poll changed failed OFF back to ON")
	}
	m.stopWorkerFn = func(context.Context) error { return nil }
	m.button = 0
	if retry := m.statusAction(); retry == nil || !strings.Contains(ansi.Strip(m.managementStatus()), "Cancelling") {
		t.Fatal("failed OFF could not be retried with Enter")
	}
}

func TestStopRequestReportsLongRunningDaemon(t *testing.T) {
	m := modelFixture(t)
	m.worker.State = "running"
	m.workerEnabled = false
	m.stopRequestedAt = time.Now().Add(-time.Minute - time.Second)
	m.Update(tickMsg(time.Now()))
	if len(m.errors) != 1 || m.errors[0].code != store.CodeDaemonUnreachable {
		t.Fatalf("missing delayed-stop error: %+v", m.errors)
	}
	m.Update(tickMsg(time.Now()))
	if len(m.errors) != 1 || m.errors[0].count != 1 {
		t.Fatalf("delayed-stop error repeated: %+v", m.errors)
	}
	m.Update(workerMsg{status: store.WorkerStatus{State: "stopped"}, control: &store.WorkerControl{Stopped: true}, initialized: true})
	if len(m.errors) != 0 {
		t.Fatalf("stop did not clear delayed-stop error: %+v", m.errors)
	}
}
