package panel

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func (m *model) key(key tea.KeyPressMsg) tea.Cmd {
	m.diagnosticClickGroup = ""
	k := key.String()
	if !m.popupQuitKey(k) {
		m.quitUntil = time.Time{}
	}
	cancelDrag := k == "esc" && m.textSelection != nil && m.textSelection.pressed
	m.clearTextSelection()
	if cancelDrag {
		return nil
	}
	if m.helpOpen {
		return m.helpKey(key)
	}
	if m.input == nil && m.dialog == "" && k == "?" {
		m.helpOpen, m.helpScroll = true, 0
		return nil
	}
	m.reveal = true
	if m.input != nil {
		return m.inputKey(key)
	}
	// When Hint joins the viewport, page keys scroll its complete explanation without moving
	// the selected sandbox, action, or confirmation choice.
	if m.scrollHint(k) {
		return nil
	}
	if m.dialog != "" {
		return m.dialogKey(k)
	}
	if m.popupQuitKey(k) {
		return m.confirmQuit()
	}
	return m.normalKey(k)
}
func (m *model) move(key string) {
	m.clearNotice()
	delta := 1
	if movingUp(key) {
		delta = -1
	}
	if key == "pgup" {
		delta = -max(1, m.height-10)
	}
	if key == "pgdown" {
		delta = max(1, m.height-10)
	}
	switch m.focus {
	case "events":
		m.moveEvents(key, delta)
		return
	case "diagnostics":
		m.moveDiagnostics(key, delta)
		return
	case "errors":
		m.moveErrors(key, delta)
		return
	}
	if m.focus == "table" && (movingDown(key)) && (len(m.errors) > 0 || len(m.events) > 0) && m.atMainEnd() {
		m.focusFollowingSections()
		return
	}

	if m.detailsFocused() {
		m.moveDetails(key, delta)
		return
	}

	m.movePage(key, delta)
}

func (m *model) atMainEnd() bool {
	switch m.page {
	case "Sandboxes":
		rows := m.visibleRows()
		if len(rows) == 0 {
			return true
		}
		last := rows[len(rows)-1]
		if m.selected != last.Sandbox.ID {
			return false
		}

		return m.focus == "table"
	case "Plugins":
		return m.focus == "table" && m.pluginIndex >= len(m.visiblePlugins())-1
	case "Status":
		return m.focus == "table" && m.button >= len(m.statusActions())-1
	}
	return false
}

func (m *model) focusMainEnd() {
	m.focus, m.reveal = "table", true
	switch m.page {
	case "Sandboxes":
		rows := m.visibleRows()
		if len(rows) == 0 {
			m.scroll = 0
			return
		}
		last := rows[len(rows)-1]
		m.selected = last.Sandbox.ID
		m.expanded = m.selected

	case "Plugins":
		m.pluginIndex = max(0, len(m.visiblePlugins())-1)
		m.ensureSelection()
		if len(m.visiblePlugins()) == 0 {
			m.scroll = 0
		}
	case "Status":
		m.button = len(m.statusActions()) - 1
	default:
		m.scroll = 0
	}
}

func (m *model) activate() tea.Cmd {
	switch m.focus {
	case "events":
		m.eventsExpanded = !m.eventsExpanded
		m.eventIndex = -1
		return nil
	case "diagnostics":
		m.toggleDiagnostics()
		return nil
	case "errors":
		return m.recoverError()
	case "plugins":
		return m.changeOverride(false)
	case "actions":
		return m.activateSandboxAction()
	}
	switch m.page {
	case "Sandboxes":
		m.clearNotice()
		if row, ok := m.row(); ok {
			m.expanded = m.selected
			if len(m.sandboxActions(row)) > 0 {
				m.focus, m.button = "actions", 0
			} else if len(m.plugins) > 0 {
				m.focus, m.detailIndex = "plugins", 0
			}
		}
	case "Plugins":
		return m.toggleDefault()
	case "Status":
		return m.statusAction()

	}
	return nil
}
func (m *model) focusErrors() {
	m.clearNotice()
	if len(m.errors) == 0 {
		return
	}
	m.focus = "errors"
	if len(m.visibleErrors()) == 0 {
		m.focus = "diagnostics"
	}
	m.errorIndex = min(m.errorIndex, max(0, len(m.visibleErrors())-1))
	m.reveal = true
}

func (m *model) recoverError() tea.Cmd {
	entries := m.visibleErrors()
	if len(entries) == 0 {
		return nil
	}
	e := entries[min(m.errorIndex, len(entries)-1)]
	for _, r := range m.rows {
		if diagnosticMatchesRow(e.uiError, r) {
			m.focusSandboxRecovery(e.uiError, r)
			return nil
		}
	}
	m.openTab("Status")
	if e.code == store.CodeSSHSetup {
		for i, action := range m.statusActions() {
			if action.id == "ssh" {
				m.button = i
				break
			}
		}
	}
	return nil
}
func diagnosticMatchesRow(e uiError, r registration.SandboxRow) bool {
	return r.Sandbox.ID == e.scope || r.Scope == e.scope || store.StatusKey(r.Scope, r.Sandbox.ID) == e.scope
}
func recoveryAction(e uiError, r registration.SandboxRow) string {
	switch e.code {
	case store.CodeManagement:
		if canRemoveEntry(r) {
			return "remove"
		}
		if !r.EntryPresent {
			return "enable"
		}
		return "disable"
	case store.CodeSafeEntryUnsupported:
		return "enable"
	case store.CodeHerdrCompatibility, store.CodeHerdrFix:
		return "fix-herdr"
	case store.CodeRename:
		return "rename"
	}
	return "resync"
}
func (m *model) focusSandboxRecovery(e uiError, r registration.SandboxRow) {
	m.selected, m.expanded, m.page = r.Sandbox.ID, r.Sandbox.ID, "Sandboxes"
	m.focus = "actions"
	if e.code == store.CodePluginChoice {
		m.focus = "plugins"
		return
	}
	action := recoveryAction(e, r)
	m.focus = "table"
	for i, item := range m.sandboxActions(r) {
		if item.id == action {
			m.focus, m.button = "actions", i
			break
		}
	}
}

func (m *model) toggleDefault() tea.Cmd {
	items := m.visiblePlugins()
	if len(items) == 0 {
		return nil
	}
	p := items[m.pluginIndex]
	enabled := !p.Enabled
	if enabled && p.UnsupportedReason != "" {
		m.notice = "Cannot turn on " + p.ID + ": " + p.UnsupportedReason
		return nil
	}
	change := pluginChoiceChange{target: "plugin:" + p.ID, pluginID: p.ID, enabled: &enabled, defaultChoice: true}
	return m.savePluginChoice(change.target, change, func() error { return store.SetGuestPluginDefault(p.ID, enabled) })
}
func (m *model) retry() tea.Cmd {
	r, ok := m.row()
	if !ok || r.Enabled != store.Enabled || !strings.EqualFold(r.Sandbox.Status, "running") || r.AssociationIssue != "" {
		return nil
	}
	return m.run(r.Sandbox.ID, store.CodePreparation, "Preparation completed", func() error {
		return m.service.registration.RetrySelected(context.Background(), io.Discard, registration.SandboxSelection{Scope: r.Scope, Socket: m.checks.Socket, ID: r.Sandbox.ID, Name: r.Sandbox.Name})
	})
}
func (m *model) dialogKey(k string) tea.Cmd {
	if k == "esc" || k == "ctrl+c" {
		if m.dialog == "fix-herdr" {
			m.closeHerdrFixConfirmation()
		} else {
			m.dialog = ""
		}
		return nil
	}
	if k == "tab" || k == "right" {
		m.button++
		return nil
	}
	if k == "shift+tab" || k == "left" {
		m.button = max(0, m.button-1)
		return nil
	}
	if k != "enter" {
		return nil
	}
	return m.confirmDialog()
}

// Confirmation choices have their own index. Restore the initiating action by
// identity so their Cancel/Fix index cannot select a destructive Details action.
func (m *model) closeHerdrFixConfirmation() {
	target := m.dialogTarget
	m.dialog, m.dialogTarget, m.dialogReason = "", "", ""
	m.focus, m.button = "table", 0
	if row, ok := findRow(m.rows, target); ok {
		for i, action := range m.sandboxActions(row) {
			if action.id == "fix-herdr" {
				m.selected, m.focus, m.button = target, "actions", i
				return
			}
		}
	}
}

func (m *model) inputKey(key tea.KeyPressMsg) tea.Cmd {
	in := m.input
	k := key.String()
	if strings.Contains(m.notice, " or Esc cancels before ") {
		m.notice = ""
	}
	runes := []rune(in.text)
	switch k {
	case "esc", "ctrl+c":
		m.input = nil
		return nil
	case "ctrl+u":
		in.text = ""
		in.cursor = 0
	case "left":
		in.cursor = max(0, in.cursor-1)
	case "right":
		in.cursor = min(len(runes), in.cursor+1)
	case "home":
		in.cursor = 0
	case "end":
		in.cursor = len(runes)
	case "backspace":
		if in.cursor > 0 {
			in.text = string(append(runes[:in.cursor-1], runes[in.cursor:]...))
			in.cursor--
		}
	case "delete":
		if in.cursor < len(runes) {
			in.text = string(append(runes[:in.cursor], runes[in.cursor+1:]...))
		}
	case "enter":
		return m.submitInput(in)
	default:
		if key.Text != "" {
			text := []rune(safeText(key.Text))
			runes = append(runes[:in.cursor], append(text, runes[in.cursor:]...)...)
			in.text = string(runes)
			in.cursor += len(text)
		}
	}
	if m.input != nil && in.kind == "filter" {
		m.queries[m.page] = in.text
		m.ensureSelection()
		m.scroll = 0
	}
	return nil
}
func (m *model) hitAction(h hit) tea.Cmd {
	m.reveal = true
	if h.action != "error" {
		m.diagnosticClickGroup = ""
	}
	if m.input != nil {
		m.notice = m.blockedInputHint(h)
		return nil
	}
	if m.dialog != "" {
		if h.action == "dialog" {
			m.button = 0
			fmt.Sscanf(h.id, "%d", &m.button)
			return m.dialogKey("enter")
		}
		return nil
	}
	m.clearNotice()
	return m.activateHit(h)
}

func (m *model) openTab(page string) {
	m.clearNotice()
	if page == "Errors" {
		m.focusErrors()
		return
	}
	m.previous = m.page
	m.page = page
	m.focus = "table"
	m.scroll = 0
	m.button = 0
	m.reveal = true
}
func (m *model) cycleTab(delta int) {
	pages := []string{"Sandboxes", "Status", "Plugins"}
	index := 0
	for i, page := range pages {
		if page == m.page {
			index = i
		}
	}
	m.openTab(pages[(index+delta+len(pages))%len(pages)])
}
func (m *model) statusAction() tea.Cmd {
	m.clearNotice()
	actions := m.statusActions()
	if m.button < 0 || m.button >= len(actions) {
		return nil
	}
	switch actions[m.button].id {
	case "toggle":
		if m.workerControlError {
			return m.setWorkerEnabled(m.workerEnabled)
		}
		return m.setWorkerEnabled(!m.workerEnabled)
	case "ssh":
		m.dialog = "ssh"
		m.button = 0
	case "refresh":
		return m.observe()
	}
	return nil
}

func (m *model) setWorkerEnabled(enabled bool) tea.Cmd {
	m.workerIntentSet = true
	if m.workerEnabled == enabled && !m.workerControlError {
		if !enabled {
			if err := store.SetWorkerControl(true); err != nil {
				m.workerControlError = true
				m.addError(store.CodeDaemonStop, "worker", err)
			}
		}
		return nil
	}
	m.workerEnabled = enabled
	m.workerControlError = false
	if !m.persistWorkerChoice(enabled) {
		return nil
	}
	if m.pending["worker"] {
		m.pendingWorkerChoiceNotice(enabled)
		return nil
	}
	return m.applyWorkerChoice(enabled)
}

func (m *model) applyWorkerChoice(enabled bool) tea.Cmd {
	if enabled && !m.managementCancelling() {
		return m.startWorker()
	}
	m.workerCommandEnabled = false
	m.workerControlError = false
	m.stopRequestedAt = time.Now()
	m.stopDelayReported = false
	return m.run("worker", store.CodeDaemonStop, "Management stop requested", func() error {
		return m.stopWorkerFn(context.Background())
	})
}

func (m *model) blockedInputHint(h hit) string {
	current, finish := m.inputDescription()
	target := m.hitDescription(h)
	return current + "; " + finish + " or Esc cancels before " + target + "."
}

func (m *model) normalKey(k string) tea.Cmd {
	switch k {
	case "q", "ctrl+c":
		return m.quit()
	case "e":
		m.focusErrors()
		return nil
	case "s", "p", "b":
		page := map[string]string{"s": "Status", "b": "Status", "p": "Plugins"}[k]
		m.openTab(page)
		return nil
	case "c":
		m.clearNotice()
		return m.observe()
	case "/":
		return m.beginFilter()

	case "tab", "shift+tab":
		m.cycleTabKey(k)
		return nil
	case "left", "right":
		return m.horizontalKey(k)
	case "esc":
		return m.escapeKey(k)
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		m.move(k)
		return nil
	case "enter":
		return m.activate()
	case "r":
		if m.page == "Sandboxes" && m.focus == "table" {
			return m.beginRename()
		}
	case "d":
		return m.dismissError()
	case "n":
		if m.focus == "diagnostics" || m.focus == "errors" {
			m.toggleDiagnostics()
		}
	case "space", " ":
		return m.spaceKey(k)
	case "i":
		if m.focus == "plugins" {
			return m.changeOverride(true)
		}
	}
	return nil
}

func (m *model) horizontalKey(k string) tea.Cmd {
	if m.focus == "diagnostics" {
		if m.showAllErrors != (k == "right") {
			m.toggleDiagnostics()
		}
		return nil
	}
	if m.page != "Sandboxes" {
		return nil
	}
	return m.sandboxHorizontalKey(k)
}

func (m *model) moveActionHorizontally(k string) {
	if row, ok := m.row(); ok {
		delta := 1
		if k == "left" {
			delta = -1
		}
		m.button = max(0, min(len(m.sandboxActions(row))-1, m.button+delta))
	}
}

func (m *model) escapeKey(k string) tea.Cmd {
	m.clearNotice()
	if m.focus == "errors" || m.focus == "diagnostics" || m.focus == "events" {
		m.focus = "table"
		m.scroll = 0
		return nil
	}
	if m.page != "Sandboxes" {
		m.openTab("Sandboxes")
		m.ensureSelection()
	} else if m.focus != "table" {
		m.focus = "table"
	} else {
		m.queries[m.page] = ""
		m.ensureSelection()
	}
	return nil
}

func (m *model) spaceKey(k string) tea.Cmd {
	if m.page == "Status" && m.focus == "table" && m.button == 0 {
		return m.statusAction()
	}
	if m.page == "Sandboxes" && (m.focus == "table" || m.focus == "actions") {
		return m.toggleEnabled()
	}
	if m.focus == "events" {
		m.eventsExpanded = !m.eventsExpanded
		m.eventIndex = -1
		return nil
	}
	if m.focus == "diagnostics" {
		m.toggleDiagnostics()
		return nil
	}
	if m.page == "Plugins" && m.focus == "table" {
		return m.toggleDefault()
	}
	if m.focus == "plugins" {
		return m.changeOverride(false)
	}
	return nil
}

func (m *model) moveEvents(key string, delta int) {
	if key == "home" {
		m.eventIndex = -1
		return
	}
	if key == "end" && m.eventsExpanded {
		m.eventIndex = len(m.events) - 1
		return
	}
	if (movingUp(key)) && m.eventIndex < 0 {
		m.focusMainEnd()
		return
	}
	if paging(key) {
		m.reveal = false
		m.scroll = max(0, m.scroll+delta)
		return
	}
	if delta < 0 {
		m.eventIndex--
		return
	}
	if m.eventsExpanded && m.eventIndex < len(m.events)-1 {
		m.eventIndex++
		return
	}
	if len(m.errors) > 0 {
		m.focus = "diagnostics"
		return
	}
	m.reveal = false
	m.scroll = max(0, m.scroll+delta)
	return
}

func (m *model) moveDiagnostics(key string, delta int) {
	if movingUp(key) {
		if len(m.events) > 0 {
			m.focus = "events"
			m.eventIndex = -1
			if m.eventsExpanded {
				m.eventIndex = len(m.events) - 1
			}
		} else {
			m.focusMainEnd()
		}
		return
	}
	if (movingDown(key)) && len(m.visibleErrors()) > 0 {
		m.focus, m.errorIndex = "errors", 0
		return
	}
	if paging(key) {
		m.reveal = false
		m.scroll = max(0, m.scroll+delta)
	}
	return
}

func (m *model) moveErrors(key string, delta int) {
	entries := m.visibleErrors()
	if (movingUp(key)) && m.errorIndex == 0 {
		m.focus = "diagnostics"
		return
	}
	if key == "home" {
		m.errorIndex = 0
		return
	}
	if key == "end" {
		m.errorIndex = max(0, len(entries)-1)
		return
	}
	if key != "pgup" && key != "pgdown" && len(entries) > 0 && ((delta > 0 && m.errorIndex < len(entries)-1) || (delta < 0 && m.errorIndex > 0)) {
		m.errorIndex += delta
		return
	}
	m.reveal = false
	m.scroll = max(0, m.scroll+delta)
	return
}

func (m *model) moveDetails(key string, delta int) {
	row, _ := m.row()
	actionCount := len(m.sandboxActions(row))
	switch key {
	case "home":
		m.firstDetail(actionCount)
		return
	case "end":
		m.lastDetail(actionCount)
		return
	}
	if m.focus == "actions" {
		if delta > 0 && len(m.plugins) > 0 {
			m.focus, m.detailIndex = "plugins", 0
		}
	} else {
		next := m.detailIndex + delta
		if next < 0 && actionCount > 0 {
			m.focus = "actions"
		} else {
			m.detailIndex = max(0, min(len(m.plugins)-1, next))
		}
	}
	return
}

func (m *model) moveSandbox(key string, delta int) {
	rows := m.visibleRows()
	if len(rows) == 0 {
		return
	}
	index := 0
	for i, r := range rows {
		if r.Sandbox.ID == m.selected {
			index = i
		}
	}
	if key == "home" {
		index = 0
	} else if key == "end" {
		index = len(rows) - 1
	} else {
		index = max(0, min(len(rows)-1, index+delta))
	}
	if m.selected != rows[index].Sandbox.ID {
		m.clearNotice()
		m.focus = "table"
	}
	m.selected = rows[index].Sandbox.ID
	m.expanded = m.selected
	m.scroll = max(0, index-max(1, m.height-15))
}

func (m *model) activateSandboxAction() tea.Cmd {
	r, ok := m.row()
	if !ok {
		return nil
	}
	actions := m.sandboxActions(r)
	if m.button < 0 || m.button >= len(actions) {
		return nil
	}
	switch actions[m.button].id {
	case "enable":
		return m.setEnabled(store.Enabled)
	case "disable":
		return m.setEnabled(store.Disabled)
	case "remove":
		return m.removeEntry()
	case "resync":
		return m.retry()
	case "rename":
		return m.beginRename()
	case "fix-herdr":
		if m.pending[r.Sandbox.ID] {
			return nil
		}
		m.dialog, m.dialogTarget, m.button = "fix-herdr", r.Sandbox.ID, 0
		m.dialogReason = m.hostHerdrVersion()
		return nil
	}
	return nil
}

func (m *model) submitInput(in *inputState) tea.Cmd {
	switch in.kind {
	case "filter":
		m.input = nil
	case "rename":
		r, ok := findRow(m.rows, in.target)
		if !ok {
			m.addError(store.CodeRename, in.target, fmt.Errorf("registration is no longer available"))
			return nil
		}
		label := strings.TrimSpace(in.text)
		if label == "" {
			m.addError(store.CodeRename, in.target, fmt.Errorf("label cannot be empty"))
			return nil
		}
		if m.pending[in.target] {
			return nil
		}
		if label == r.Label {
			m.input = nil
			return nil
		}
		m.pending[in.target] = true
		m.actionNotice(in.target, store.CodeRename)
		submittedText := in.text
		return func() tea.Msg {
			return operationMsg{target: in.target, code: store.CodeRename, notice: "herdr label saved", err: m.service.registration.Rename(context.Background(), r.Scope, r.Sandbox.ID, label), rename: true, input: in, submittedText: submittedText}
		}

	}
	return nil
}

func (m *model) confirmDialog() tea.Cmd {
	switch m.dialog {
	case "startup":
		m.dialog = ""
		if m.button%2 == 1 {
			return m.startWorker()
		}

	case "ssh":
		if m.button%2 == 0 {
			m.dialog = ""
			return nil
		}
		m.dialog = ""
		return tea.Exec(sshCommand{m.service}, func(err error) tea.Msg {
			return operationMsg{target: "Host", code: store.CodeSSHSetup, notice: "SSH setup finished", err: err}
		})
	case "fix-herdr":
		approved := m.button%2 == 1
		r, ok := findRow(m.rows, m.dialogTarget)
		host := m.dialogReason
		m.closeHerdrFixConfirmation()
		if !approved || !ok || !m.canFixHerdr(r) {
			return nil
		}
		return m.run(r.Sandbox.ID, store.CodeHerdrFix, "herdr fix completed", func() error {
			return m.service.registration.FixGuestHerdr(context.Background(), io.Discard, registration.SandboxSelection{Scope: r.Scope, Socket: m.checks.Socket, ID: r.Sandbox.ID, Name: r.Sandbox.Name}, host)
		})
	}
	return nil
}

func (m *model) activateHit(h hit) tea.Cmd {
	switch h.action {
	case "tab-prev":
		m.cycleTab(-1)
	case "tab-next":
		m.cycleTab(1)
	case "enabled":
		m.selected, m.expanded, m.focus = h.id, h.id, "table"
		return m.toggleEnabled()
	case "select", "expand":
		m.selected, m.expanded, m.focus = h.id, h.id, "table"
		return nil
	case "filter":
		m.input = &inputState{kind: "filter", text: m.queries[m.page], cursor: len([]rune(m.queries[m.page]))}
	case "clear":
		m.queries[m.page] = ""
		m.ensureSelection()
	case "page":
		m.openTab(h.id)
	case "worker":
		m.openTab("Status")
	case "default":
		return m.hitDefaultPlugin(h)
	case "plugin":
		return m.hitDefaultPlugin(h)
	case "override":
		return m.hitOverridePlugin(h)
	case "action":
		return m.hitSandboxAction(h)
	case "ssh":
		m.dialog = "ssh"
		m.button = 0
	case "event-section":
		m.focus, m.eventIndex = "events", -1
		m.eventsExpanded = !m.eventsExpanded
	case "event":
		m.focus = "events"
		fmt.Sscanf(h.id, "%d", &m.eventIndex)
	case "diagnostic-toggle":
		m.toggleDiagnostics()
	case "error-section":
		m.focusErrors()
	case "error":
		return m.hitDiagnostic(h)
	case "docker":
		m.openTab("Status")
	case "status-action":
		m.openTab("Status")
		fmt.Sscanf(h.id, "%d", &m.button)
		return m.statusAction()
	case "status-choice":
		m.openTab("Status")
		return m.setWorkerEnabled(h.id == "on")
	case "refresh":
		return m.observe()
	}
	return nil
}

func (m *model) scrollHint(k string) bool {
	if m.hintInBody && (k == "pgup" || k == "pgdown") {
		delta := max(1, m.height-4)
		if k == "pgup" {
			delta = -delta
		}
		m.scroll = max(0, m.scroll+delta)
		m.reveal = false
		return true
	}
	return false
}

func (m *model) movePage(key string, delta int) {
	if m.page == "Plugins" {
		n := len(m.visiblePlugins())
		if key == "home" {
			m.pluginIndex = 0
		} else if key == "end" {
			m.pluginIndex = max(0, n-1)
		} else {
			m.pluginIndex = max(0, min(n-1, m.pluginIndex+delta))
		}
		m.ensureSelection()
		m.scroll = max(0, m.pluginIndex-max(1, m.height-14))
		return
	}
	if m.page == "Status" {
		switch key {
		case "home":
			m.button = 0
		case "end":
			m.button = len(m.statusActions()) - 1
		default:
			m.button = max(0, min(len(m.statusActions())-1, m.button+delta))
		}
		m.focus = "table"
		return
	}

	if m.page != "Sandboxes" {
		m.scroll = max(0, m.scroll+delta)
		return
	}
	m.moveSandbox(key, delta)
}

func (m *model) persistWorkerChoice(enabled bool) bool {
	// Persist OFF at acceptance so a queued or running start cannot schedule more work.
	if !enabled {
		m.stopRequestedAt = time.Now()
		m.stopDelayReported = false
		if err := store.SetWorkerControl(true); err != nil {
			m.workerControlError = true
			m.addError(store.CodeDaemonStop, "worker", fmt.Errorf("Could not save Management OFF: %w", err))
			return false
		}
	} else {
		saved, err := store.ReadStatusStore()
		if err != nil {
			m.workerControlError = true
			m.addError(store.CodeDaemonStart, "worker", err)
			return false
		}
		m.workerStartGeneration = 0
		if saved.WorkerControl != nil {
			m.workerStartGeneration = saved.WorkerControl.Generation
		}
	}

	return true
}

func (m *model) pendingWorkerChoiceNotice(enabled bool) {
	if enabled == m.workerCommandEnabled {
		code := store.CodeDaemonStop
		if enabled {
			code = store.CodeDaemonStart
		}
		m.actionNotice("worker", code)
	} else {
		m.notice, m.noticeTarget = "Cancelling", "worker"
		if enabled {
			m.notice = "Cancelling. Management will read current sandbox and herdr state before resuming."
		}
	}
}

func (m *model) inputDescription() (string, string) {
	in := m.input
	current := "Editing input"
	finish := "Enter confirms"
	switch in.kind {
	case "filter":
		current = "Filtering " + safeText(m.page)
		finish = "Enter accepts"
	case "rename":
		name := in.target
		if row, ok := findRow(m.rows, in.target); ok {
			name = row.Sandbox.Name
		}
		current = "Renaming herdr label for " + safeText(name)
		finish = "Enter saves"
	}
	return current, finish
}

func (m *model) hitDescription(h hit) string {
	target := "using that control"
	switch h.action {
	case "tab-prev", "tab-next":
		target = "switching tabs"
	case "page":
		target = "opening " + safeText(h.id)
	case "select", "expand":
		if row, ok := findRow(m.rows, h.id); ok {
			target = "opening " + safeText(row.Sandbox.Name)
		} else {
			target = "opening a sandbox"
		}
	case "filter":
		target = "editing the filter"
	case "clear":
		target = "clearing the filter"
	case "worker", "docker":
		target = "opening Status"
	case "default", "plugin":
		target = "changing herdr plugin sync for " + safeText(h.id)
	case "override":
		target = "changing " + safeText(h.id) + " for this sandbox"
	case "action":
		return m.actionDescription(h)
	case "ssh":
		target = "opening SSH setup"
	case "error":
		target = "opening an error"
	case "status-action":
		actions := m.statusActions()
		var index int
		if _, err := fmt.Sscanf(h.id, "%d", &index); err == nil && index >= 0 && index < len(actions) {
			target = map[string]string{"toggle": "changing Management", "ssh": "opening SSH setup", "refresh": "refreshing status"}[actions[index].id]
		}
	case "status-choice":
		target = "setting Management " + strings.ToUpper(h.id)
	case "refresh":
		target = "refreshing observations"
	}
	return target
}

func (m *model) hitDefaultPlugin(h hit) tea.Cmd {
	m.pluginSelected = h.id
	m.pluginIndex = 0
	for i, p := range m.visiblePlugins() {
		if p.ID == h.id {
			m.pluginIndex = i
		}
	}
	return m.toggleDefault()
}

func (m *model) hitOverridePlugin(h hit) tea.Cmd {
	if _, ok := findRow(m.rows, m.expanded); !ok {
		return nil
	}
	m.selected = m.expanded
	for i, p := range m.plugins {
		if p.ID == h.id {
			m.detailIndex = i
		}
	}
	m.focus = "plugins"
	return m.changeOverride(false)
}

func (m *model) hitSandboxAction(h hit) tea.Cmd {
	if _, ok := findRow(m.rows, m.expanded); !ok {
		return nil
	}
	m.selected = m.expanded
	m.focus = "actions"
	row, _ := m.row()
	for i, action := range m.sandboxActions(row) {
		if action.id == h.id {
			m.button = i
			return m.activate()
		}
	}
	return nil
}

func (m *model) hitDiagnostic(h hit) tea.Cmd {
	var index int
	if _, err := fmt.Sscanf(h.id, "%d", &index); err != nil {
		return nil
	}
	entries := m.visibleErrors()
	if index < 0 || index >= len(entries) {
		return nil
	}
	group := entries[index]
	identity := diagnosticGroupKey(group.code, group.scope)
	selected := m.focus == "errors" && m.errorIndex == index && m.diagnosticClickGroup == identity
	m.focus = "errors"
	m.errorIndex = index
	m.diagnosticClickGroup = identity
	if selected {
		m.diagnosticClickGroup = ""
		return m.recoverError()
	}
	return nil
}

func movingUp(key string) bool   { return key == "up" || key == "k" }
func movingDown(key string) bool { return key == "down" || key == "j" }
func paging(key string) bool     { return key == "pgup" || key == "pgdown" }

func (m *model) firstDetail(actionCount int) {
	if actionCount > 0 {
		m.focus, m.button = "actions", 0
	} else {
		m.detailIndex = 0
	}
}
func (m *model) lastDetail(actionCount int) {
	if len(m.plugins) > 0 {
		m.focus, m.detailIndex = "plugins", len(m.plugins)-1
	} else {
		m.button = max(0, actionCount-1)
	}

}

func (m *model) detailsFocused() bool {
	return m.page == "Sandboxes" && m.expanded == m.selected && (m.focus == "actions" || m.focus == "plugins")
}

func (m *model) beginFilter() tea.Cmd {
	m.clearNotice()
	if m.page == "Sandboxes" || m.page == "Plugins" {
		m.input = &inputState{kind: "filter", text: m.queries[m.page], cursor: len([]rune(m.queries[m.page]))}
	}
	return nil
}

func (m *model) actionDescription(h hit) string {
	target := "using that control"
	if row, ok := findRow(m.rows, m.expanded); ok {
		for _, action := range m.sandboxActions(row) {
			if action.id == h.id {
				target = "running " + safeText(action.label)
				break
			}
		}
	}

	return target
}

func (m *model) cycleTabKey(k string) {
	delta := 1
	if k == "shift+tab" {
		delta = -1
	}
	m.cycleTab(delta)
}

func (m *model) sandboxHorizontalKey(k string) tea.Cmd {
	switch m.focus {
	case "table":
		if k == "right" {
			return m.activate()
		}
	case "plugins":
		if k == "left" {
			m.clearNotice()
			m.focus = "table"
		}
	case "actions":
		if k == "left" && m.button == 0 {
			m.clearNotice()
			m.focus = "table"
		} else {
			m.moveActionHorizontally(k)
		}
	}
	return nil
}
