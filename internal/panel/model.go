package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
)

type observationMsg struct {
	generation     int
	checks         precheck.Result
	rows           []registration.SandboxRow
	plugins        []guestplugins.Choice
	settings       store.GuestPluginDefaults
	err, pluginErr error
}
type checksMsg struct {
	generation int
	checks     precheck.Result
}

type workerMsg struct {
	status      store.WorkerStatus
	initialized bool
	control     *store.WorkerControl
	errors      map[string]store.ErrorRecord
	events      []store.EventRecord
	observedAt  time.Time
	err         error
}
type errorWrite struct {
	key       string
	revision  int
	apply     func() error
	dismissal *uiError
	err       error
}
type errorChange struct {
	revision    int
	persistedAt time.Time
}
type errorsPersistedMsg struct {
	writes []errorWrite
	failed []errorWrite
	at     time.Time
}
type tickMsg time.Time
type operationMsg struct {
	target, code, notice string
	err                  error
	rename               bool
	input                *inputState
	submittedText        string
	pluginChoice         *pluginChoiceChange
	enabledChoice        *store.EnabledChoice
}
type pluginChoiceChange struct {
	target, pluginID string
	enabled          *bool
	defaultChoice    bool
}
type uiError struct {
	ref                                 string
	code, scope, detail                 string
	pluginID, command, recovery, output string
	count                               int
	seen, firstSeen                     time.Time
	dismissed                           bool
}
type inputState struct {
	kind, target, text string
	cursor             int
}
type hit struct {
	x, y, w    int
	action, id string
}
type line struct {
	text string
	hits []hit
}

// model keeps terminal state separate from operations. Identity, rather than a
// row index, anchors selection and input while observations arrive.
type model struct {
	service                                                                         *Service
	startWorkerFn                                                                   func() error
	stopWorkerFn                                                                    func(context.Context) error
	setEnabledFn                                                                    func(context.Context, string, string, store.EnabledChoice) error
	removeEntryFn                                                                   func(context.Context, string, string) error
	helpOpen                                                                        bool
	helpScroll                                                                      int
	page, previous, focus                                                           string
	width, height, scroll, pluginIndex, detailIndex, errorIndex, button, generation int
	rows                                                                            []registration.SandboxRow
	plugins                                                                         []guestplugins.Choice
	settings                                                                        store.GuestPluginDefaults
	checks                                                                          precheck.Result
	worker                                                                          store.WorkerStatus
	selected, expanded, pluginSelected                                              string
	diagnosticClickGroup                                                            string
	queries                                                                         map[string]string
	input                                                                           *inputState
	dialog, dialogTarget, dialogReason                                              string
	errors                                                                          []uiError
	events                                                                          []store.EventRecord
	eventsExpanded, showAllErrors                                                   bool
	eventIndex                                                                      int
	pending                                                                         map[string]bool
	pendingChoices                                                                  map[string]pluginChoiceChange
	pendingEnabled                                                                  map[string]store.EnabledChoice
	loading, refreshing, workerLoaded                                               bool
	checksPending                                                                   bool
	workerEnabled                                                                   bool
	workerIntentSet                                                                 bool
	workerCommandEnabled                                                            bool
	workerStartGeneration                                                           uint64
	workerControlError                                                              bool
	stopRequestedAt                                                                 time.Time
	stopDelayReported                                                               bool
	closing                                                                         bool
	effects                                                                         []errorWrite
	writingErrors                                                                   bool
	errorRevision                                                                   int
	errorChanges                                                                    map[string]errorChange
	lastErrorsObserved                                                              time.Time
	hintInBody                                                                      bool
	reveal                                                                          bool
	ticks                                                                           int
	notice                                                                          string
	noticeTarget                                                                    string
	hits                                                                            []hit
	renderedFrame                                                                   string
	textSelection                                                                   *textSelection
}

func newModel(s *Service) *model {
	m := &model{service: s, page: "Sandboxes", focus: "table", width: 110, height: 32, queries: map[string]string{}, pending: map[string]bool{}, pendingChoices: map[string]pluginChoiceChange{}, workerEnabled: true, loading: true, worker: store.WorkerStatus{State: "starting"}}
	if saved, err := store.ReadStatusStore(); err == nil {
		if saved.WorkerControl != nil {
			m.workerEnabled = !saved.WorkerControl.Stopped
		}
		for _, item := range saved.Errors {
			if (item.Scope == "worker" && (item.Code == store.CodeDaemonUnreachable || item.Code == store.CodeDaemonStart || item.Code == store.CodeDaemonStop)) || (item.Scope == "Host" && item.Code == store.CodeDaemonUnreachable) {
				m.workerControlError = true
				break
			}
		}
	}
	if s != nil {
		m.startWorkerFn = s.lifecycle.StartWorkerExplicit
		m.stopWorkerFn = s.lifecycle.WaitWorkerStopped
		m.setEnabledFn = s.registration.SetEnabled
		m.removeEntryFn = s.registration.RemoveEntry
	}
	return m
}
func (m *model) Init() tea.Cmd { return tea.Batch(m.observe(), m.pollWorker(), tick()) }
func tick() tea.Cmd            { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m *model) observe() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	m.generation++
	generation := m.generation
	commands := []tea.Cmd{}
	if !m.checksPending {
		m.checksPending = true
		commands = append(commands, func() tea.Msg { return checksMsg{generation, m.service.registration.Inspect()} })
	}
	commands = append(commands, func() tea.Msg {
		daemon, err := m.service.sbx.DaemonStatus(context.Background())
		var rows []registration.SandboxRow
		if err == nil && daemon.Socket != "" {
			rows, err = m.service.registration.SandboxRows(context.Background(), daemon.Socket)
		} else if err == nil {
			err = fmt.Errorf("sandbox daemon socket is unavailable")
		}
		plugins, pluginErr := m.service.registration.GuestPluginChoices(context.Background())
		settings, settingsErr := store.ReadGuestPluginDefaults()
		if pluginErr == nil {
			pluginErr = settingsErr
		}
		return observationMsg{generation: generation, checks: precheck.Result{Socket: daemon.Socket, Scope: store.ScopeID(daemon.Socket)}, rows: rows, plugins: plugins, settings: settings, err: err, pluginErr: pluginErr}
	})
	return tea.Batch(commands...)
}
func (m *model) pollWorker() tea.Cmd {
	return func() tea.Msg {
		observedAt := time.Now()
		status, err := store.ReadWorkerStatus()
		saved, readErr := store.ReadStatusStore()
		if err == nil {
			err = readErr
		}
		initialized := saved.WorkerControl != nil && saved.WorkerControl.Initialized
		return workerMsg{status: status, initialized: initialized, control: saved.WorkerControl, errors: saved.Errors, events: saved.Events, observedAt: observedAt, err: err}
	}
}
func (m *model) run(target, code, notice string, f func() error) tea.Cmd {
	if m.pending[target] {
		return nil
	}
	m.pending[target] = true
	m.actionNotice(target, code)
	return func() tea.Msg { return operationMsg{target: target, code: code, notice: notice, err: f()} }
}

// Navigation drops feedback from the previous context; diagnostics remain durable.
func (m *model) clearNotice() {
	m.notice, m.noticeTarget = "", ""
}
func (m *model) operationInContext(target string) bool {
	if target == "worker" || target == "Host" {
		return m.page == "Status"
	}
	if strings.HasPrefix(target, "plugin:") {
		return m.page == "Plugins" && target == "plugin:"+m.pluginSelected
	}
	return m.page == "Sandboxes" && target == m.selected
}

func (m *model) actionNotice(target, code string) {
	action := map[string]string{
		store.CodeRename:        "Saving herdr label",
		store.CodePluginChoice:  "Saving herdr plugin sync choice",
		store.CodeManagement:    "Updating sandbox connection",
		store.CodePreparation:   "Re-syncing sandbox",
		store.CodeHerdrFix:      "Fixing herdr",
		store.CodePolicyCleanup: "Checking temporary access cleanup",
		store.CodeDaemonStart:   "Starting Management",
		store.CodeDaemonStop:    "Cancelling",
	}[code]
	if action == "" {
		action = "Applying changes"
	}
	if row, ok := findRow(m.rows, target); ok {
		action += " for " + safeText(row.Sandbox.Name)
	}
	m.notice, m.noticeTarget = action+"…", target
	if code == store.CodeDaemonStop {
		m.notice = "Cancelling"
	}
}

func (m *model) addError(code, scope string, err error) {
	if err == nil {
		return
	}
	detail := safeText(err.Error())
	m.queueErrorWrite(code, scope, func() error { return store.RecordError(code, scope, detail) })
	if store.SeverityForCode(code) == store.SeverityInfo {
		m.events = append(m.events, store.EventRecord{Code: code, Scope: scope, Detail: detail, At: time.Now()})
		return
	}
	for i := range m.errors {
		e := &m.errors[i]
		if e.code == code && e.scope == scope && e.pluginID == "" && e.detail == detail {
			e.count++
			e.seen = time.Now()
			return
		}
	}
	dismissed := false
	for _, group := range m.diagnosticGroups() {
		if group.code == code && group.scope == scope {
			dismissed = group.dismissed
			break
		}
	}
	m.errors = append(m.errors, uiError{code: code, scope: scope, detail: detail, count: 1, seen: time.Now(), dismissed: dismissed})
}
func errorScopeKey(code, scope, pluginID string) string {
	return code + "\x00" + scope + "\x00" + pluginID
}

func (m *model) queueErrorWrite(code, scope string, apply func() error) {
	if m.errorChanges == nil {
		m.errorChanges = map[string]errorChange{}
	}
	m.errorRevision++
	key := errorScopeKey(code, scope, "")
	m.errorChanges[key] = errorChange{revision: m.errorRevision}
	m.effects = append(m.effects, errorWrite{key: key, revision: m.errorRevision, apply: apply})
}

// Keep local mutations until a poll begun after their write completes confirms
// them. Polls and operations run concurrently, so an older snapshot is not a
// recovery result and must never erase a newly reported failure. A local
// operation overlay never hides independent per-plugin diagnostics.
func (m *model) mergeErrors(msg workerMsg) {
	observed := msg.observedAt
	if observed.IsZero() {
		observed = time.Now()
	}
	if observed.Before(m.lastErrorsObserved) {
		return
	}
	m.lastErrorsObserved = observed
	for key, change := range m.errorChanges {
		if !change.persistedAt.IsZero() && observed.After(change.persistedAt) {
			delete(m.errorChanges, key)
		}
	}
	var selected *diagnosticGroup
	if entries := m.visibleErrors(); m.focus == "errors" && m.errorIndex < len(entries) {
		e := entries[m.errorIndex]
		selected = &e
	}
	var merged []uiError
	for _, e := range m.errors {
		if _, local := m.errorChanges[errorScopeKey(e.code, e.scope, e.pluginID)]; local {
			merged = append(merged, e)
		}
	}
	for _, e := range msg.errors {
		if _, local := m.errorChanges[errorScopeKey(e.Code, e.Scope, e.PluginID)]; !local {
			merged = append(merged, uiError{ref: e.Ref, code: e.Code, scope: e.Scope, detail: e.Detail, firstSeen: e.FirstSeen, dismissed: e.Dismissed, pluginID: e.PluginID, command: e.Command, recovery: e.Recovery, output: e.Output, count: e.Count, seen: e.LastSeen})
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if !merged[i].firstSeen.Equal(merged[j].firstSeen) {
			return merged[i].firstSeen.Before(merged[j].firstSeen)
		}
		return diagnosticIdentity(merged[i]) < diagnosticIdentity(merged[j])
	})
	// Apply in-flight group dismissal to incoming children without suppressing
	// their details. A stale poll cannot make an acknowledged group new again.
	for i := range merged {
		if _, pending := m.errorChanges[diagnosticGroupKey(merged[i].code, merged[i].scope)]; pending {
			merged[i].dismissed = true
		}
	}
	m.errors = merged
	if m.diagnosticClickGroup != "" {
		found := false
		for _, e := range m.visibleErrors() {
			if diagnosticGroupKey(e.code, e.scope) == m.diagnosticClickGroup {
				found = true
				break
			}
		}
		if !found {
			m.diagnosticClickGroup = ""
		}
	}
	if selected != nil {
		for i, e := range m.visibleErrors() {
			if e.code == selected.code && e.scope == selected.scope {
				m.errorIndex = i
				break
			}
		}
	}
	m.events = msg.events
	m.errorIndex = min(m.errorIndex, max(0, len(m.visibleErrors())-1))
	if m.focus == "errors" && len(m.visibleErrors()) == 0 {
		m.focus = "diagnostics"
	}
	if m.eventIndex >= len(m.events) {
		m.eventIndex = max(-1, len(m.events)-1)
	}
	if m.focus == "events" && len(m.events) == 0 {
		m.focusMainEnd()
	}
}

func (m *model) clearError(code, scope string) {
	found := false
	for _, e := range m.errors {
		if e.code == code && e.scope == scope && e.pluginID == "" {
			found = true
		}
	}
	if found {
		m.queueErrorWrite(code, scope, func() error { return store.ClearError(code, scope) })
	}
	result := m.errors[:0]
	for _, e := range m.errors {
		if e.code != code || e.scope != scope || e.pluginID != "" {
			result = append(result, e)
		}
	}
	m.errors = result
}
func (m *model) Update(msg tea.Msg) (result tea.Model, command tea.Cmd) {
	hadErrors := len(m.errors) > 0
	defer func() {
		if m.page == "Status" && m.dialog == "" {
			if button := max(0, min(m.button, len(m.statusActions())-1)); button != m.button {
				m.button, m.reveal = button, true
			}
		}
		if hadErrors && len(m.errors) == 0 && (m.focus == "errors" || m.focus == "diagnostics") {
			m.focus, m.reveal = "table", true
			m.scroll = 0
		}
		if m.writingErrors || len(m.effects) == 0 {
			return
		}
		effects := m.effects
		m.effects = nil
		m.writingErrors = true
		persist := func() tea.Msg {
			var written, failed []errorWrite
			for _, effect := range effects {
				if err := effect.apply(); err == nil {
					written = append(written, effect)
				} else {
					effect.err = err
					failed = append(failed, effect)
				}
			}
			return errorsPersistedMsg{writes: written, failed: failed, at: time.Now()}
		}
		command = tea.Batch(command, persist)
	}()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.textSelection = nil
		m.renderedFrame = ""
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.ticks++
		if !m.workerEnabled && !m.stopRequestedAt.IsZero() && !m.stopDelayReported && time.Since(m.stopRequestedAt) > time.Minute && m.worker.State != "stopped" && m.worker.State != "exited" {
			m.addError(store.CodeDaemonUnreachable, "worker", fmt.Errorf("Cannot confirm that the daemon stopped within one minute. It may be busy or unreachable"))
			m.stopDelayReported = true
			m.workerControlError = true
		}
		var refresh tea.Cmd
		if m.ticks%5 == 0 {
			refresh = m.observe()
		}
		return m, tea.Batch(m.pollWorker(), refresh, tick())
	case errorsPersistedMsg:
		m.writingErrors = false
		dismissalFailed := false
		for _, write := range msg.failed {
			if write.dismissal == nil {
				continue
			}
			dismissalFailed = true
			for i := range m.errors {
				if m.errors[i].code == write.dismissal.code && m.errors[i].scope == write.dismissal.scope {
					m.errors[i].dismissed = false
				}
			}
			if m.errorChanges[write.key].revision == write.revision {
				delete(m.errorChanges, write.key)
			}
			m.addError(store.CodeDiagnosticDismiss, "Host", fmt.Errorf("Could not save the diagnostic dismissal. The item is still new; try Dismiss again. %v", write.err))
		}
		for _, write := range msg.writes {
			if write.dismissal != nil && !dismissalFailed {
				m.clearError(store.CodeDiagnosticDismiss, "Host")
			}
			change := m.errorChanges[write.key]
			if change.revision == write.revision {
				change.persistedAt = msg.at
				m.errorChanges[write.key] = change
			}
		}
	case workerMsg:
		if msg.err == nil {
			m.mergeErrors(msg)
		}
		m.addError(store.CodeDaemonStatus, "Host", msg.err)
		if msg.err != nil {
			m.worker.State = "error"
			m.worker.Detail = "Cannot read Management status."
		}
		if msg.err == nil {
			m.worker = msg.status
			m.clearError(store.CodeDaemonStatus, "Host")
			if msg.status.State == "reconnecting" {
				m.addError(store.CodeDaemonReconnect, "Host", fmt.Errorf("%s", msg.status.Detail))
			} else {
				m.clearError(store.CodeDaemonReconnect, "Host")
			}
			if (msg.status.State == "stopped" || msg.status.State == "exited") && !m.pending["worker"] && !m.workerEnabled {
				m.stopRequestedAt = time.Time{}
				m.stopDelayReported = false
				m.workerControlError = false
				m.clearError(store.CodeDaemonUnreachable, "worker")
				m.clearError(store.CodeDaemonUnreachable, "Host")
				m.clearError(store.CodeDaemonStop, "worker")
			} else if !m.workerEnabled && !m.workerControlError && m.stopRequestedAt.IsZero() {
				m.stopRequestedAt = time.Now()
			}
		}
		if !m.workerLoaded {
			m.workerLoaded = true
			if !m.workerIntentSet && !m.pending["worker"] && msg.err == nil && msg.status.State == "stopped" {
				if !msg.initialized {
					return m, m.startWorker()
				} else {
					m.dialog = "startup"
					m.button = 0
				}
			}
		}
	case checksMsg:
		m.checksPending = false
		m.checks = msg.checks
		return m, nil
	case observationMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.refreshing = false
		m.loading = false
		if len(m.checks.Checks) == 0 || m.checks.Socket == "" {
			m.checks.Scope, m.checks.Socket = msg.checks.Scope, msg.checks.Socket
		}
		if msg.err != nil {
			m.addError(store.CodeSandboxInventory, "Host", msg.err)
			if len(msg.rows) > 0 {
				partial := append([]registration.SandboxRow(nil), msg.rows...)
				for _, old := range m.rows {
					if _, found := findRow(partial, old.Sandbox.ID); !found {
						partial = append(partial, old)
					}
				}
				m.mergeRows(partial)
			}
			for i := range m.rows {
				m.rows[i].Status.Stale = true
			}
		} else {
			m.clearError(store.CodeSandboxInventory, "Host")
			m.mergeRows(msg.rows)
		}
		if msg.pluginErr != nil {
			m.addError(store.CodePluginDiscovery, "Host", msg.pluginErr)
		} else {
			m.clearError(store.CodePluginDiscovery, "Host")
			m.plugins = msg.plugins
			m.settings = msg.settings
		}
		m.ensureSelection()
		return m, nil
	case operationMsg:
		if msg.enabledChoice != nil {
			for i := range m.rows {
				if m.rows[i].Sandbox.ID == msg.target {
					m.rows[i].Enabled = *msg.enabledChoice
				}
			}
		}
		if _, ok := m.pendingEnabled[msg.target]; ok {
			delete(m.pendingEnabled, msg.target)
			// Discard observations started before this action completed, then refresh.
			m.generation++
			m.refreshing = false
		}
		delete(m.pending, msg.target)
		if m.noticeTarget == msg.target && !m.operationInContext(msg.target) {
			m.notice, m.noticeTarget = "", ""
		}
		if msg.target == "worker" && msg.code == store.CodeDaemonStop && msg.err == nil {
			m.stopRequestedAt = time.Time{}
			m.worker.State, m.worker.Detail = "stopped", "Daemon is stopped."
		}
		// OFF invalidates the active start even if ON was selected again before it returned.
		// Confirm the old generation settled before accepting the latest ON baseline.
		if msg.target == "worker" && msg.code == store.CodeDaemonStart && !m.stopRequestedAt.IsZero() {
			return m, m.applyWorkerChoice(m.workerEnabled)
		}
		if msg.target == "worker" && (msg.code == store.CodeDaemonStart || msg.code == store.CodeDaemonStop) && m.workerEnabled != (msg.code == store.CodeDaemonStart) {
			// The requested choice changed while this command was running. Finish
			// that command before applying the latest choice, and never let its
			// stale completion replace the newer choice's feedback.
			if msg.err == nil || msg.code == store.CodeDaemonStart {
				if msg.code == store.CodeDaemonStop {
					m.stopRequestedAt = time.Time{}
					m.worker.State, m.worker.Detail = "stopped", "Daemon is stopped."
				}
				return m, m.applyWorkerChoice(m.workerEnabled)
			}
		}
		if msg.pluginChoice != nil {
			delete(m.pendingChoices, msg.target)
			if msg.err == nil {
				m.applyPluginChoice(*msg.pluginChoice)
			}
		}
		if msg.err != nil {
			code, detail := msg.code, msg.err
			if strings.HasPrefix(msg.err.Error(), store.CodeSafeEntryUnsupported+":") {
				code = store.CodeSafeEntryUnsupported
				detail = errors.New(strings.TrimSpace(strings.TrimPrefix(msg.err.Error(), code+":")))
			}
			if code == store.CodePreparation {
				detail = fmt.Errorf("Re-sync did not finish: %v", msg.err)
			}
			if errors.Is(msg.err, store.ErrStateLocked) {
				detail = fmt.Errorf("The daemon is syncing sandbox registrations. This action did not finish; try again when syncing ends")
			}
			if msg.target == "worker" {
				if errors.Is(msg.err, lifecycle.ErrDaemonStopUnconfirmed) {
					code = store.CodeDaemonUnreachable
				} else if errors.Is(msg.err, context.DeadlineExceeded) {
					code, detail = store.CodeDaemonUnreachable, fmt.Errorf("Cannot reach daemon to change Management")
				}
			}
			if msg.target == "worker" {
				m.workerControlError = true
			}
			scope := msg.target
			if code == store.CodeSafeEntryUnsupported {
				if row, ok := findRow(m.rows, msg.target); ok {
					scope = store.StatusKey(row.Scope, msg.target)
				}
			}
			m.addError(code, scope, detail)
			if m.operationInContext(msg.target) && (m.noticeTarget == "" || m.noticeTarget == msg.target) {
				m.notice, m.noticeTarget = "", ""
				if msg.code == store.CodeHerdrFix {
					m.notice = "Fix herdr did not finish."
				}
			}
		} else {
			if msg.target == "worker" {
				m.workerControlError = false
				m.clearError(store.CodeDaemonUnreachable, "worker")
			}
			m.clearError(msg.code, msg.target)
			if m.operationInContext(msg.target) && (m.noticeTarget == "" || m.noticeTarget == msg.target) {
				m.notice, m.noticeTarget = msg.notice, ""
				if msg.target == "worker" {
					m.notice = ""
				}
			}
			if msg.rename && m.input == msg.input && (msg.submittedText == "" || m.input.text == msg.submittedText) {
				m.input = nil
			}
		}
		if m.closing && len(m.pending) == 0 {
			return m, tea.Quit
		}
		return m, tea.Batch(m.observe(), m.pollWorker())
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseClickMsg:
		m.clearTextSelection()
		if msg.Button == tea.MouseLeft {
			m.beginTextSelection(msg.X, msg.Y)
		}
	case tea.MouseMotionMsg:
		if m.textSelection != nil && m.textSelection.pressed && msg.Button == tea.MouseLeft {
			m.textSelection.move(msg.X, msg.Y)
		}
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft || msg.Button == tea.MouseNone {
			return m, m.releaseTextSelection(msg.X, msg.Y)
		}
	case tea.MouseWheelMsg:
		if m.textSelection != nil && m.textSelection.pressed {
			return m, nil
		}
		m.clearTextSelection()
		if m.helpOpen {
			if msg.Button == tea.MouseWheelUp {
				m.helpScroll = max(0, m.helpScroll-3)
			} else {
				m.helpScroll += 3
			}
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.scroll = max(0, m.scroll-3)
		} else {
			m.scroll += 3
		}
	}
	return m, nil
}
func (m *model) mergeRows(rows []registration.SandboxRow) {
	actionID := ""
	if m.focus == "actions" {
		if row, ok := m.row(); ok {
			actions := m.sandboxActions(row)
			if m.button >= 0 && m.button < len(actions) {
				actionID = actions[m.button].id
			}
		}
	}
	byID := map[string]registration.SandboxRow{}
	for _, r := range rows {
		if choice, ok := m.pendingEnabled[r.Sandbox.ID]; ok {
			r.Enabled = choice
		}
		byID[r.Sandbox.ID] = r
	}
	result := make([]registration.SandboxRow, 0, len(rows))
	for _, old := range m.rows {
		if r, ok := byID[old.Sandbox.ID]; ok {
			result = append(result, r)
			delete(byID, old.Sandbox.ID)
		}
	}
	for _, r := range rows {
		if _, ok := byID[r.Sandbox.ID]; ok {
			result = append(result, r)
			delete(byID, r.Sandbox.ID)
		}
	}
	if m.input != nil && m.input.kind == "rename" {
		if _, ok := findRow(result, m.input.target); !ok {
			m.addError(store.CodeRename, m.input.target, fmt.Errorf("registration is no longer available; the proposed label is preserved"))
		}
	}
	m.rows = result
	if actionID != "" {
		if row, ok := m.row(); ok {
			for i, action := range m.sandboxActions(row) {
				if action.id == actionID {
					m.button = i
					break
				}
			}
		}
	}
}
func findRow(rows []registration.SandboxRow, id string) (registration.SandboxRow, bool) {
	for _, r := range rows {
		if r.Sandbox.ID == id {
			return r, true
		}
	}
	return registration.SandboxRow{}, false
}
func (m *model) row() (registration.SandboxRow, bool) { return findRow(m.rows, m.selected) }
func fuzzy(query, value string) bool {
	q := []rune(strings.ToLower(query))
	if len(q) == 0 {
		return true
	}
	i := 0
	for _, r := range strings.ToLower(value) {
		if r == q[i] {
			i++
			if i == len(q) {
				return true
			}
		}
	}
	return false
}
func (m *model) visibleRows() []registration.SandboxRow {
	var rows []registration.SandboxRow
	for _, r := range m.rows {
		if fuzzy(m.queries["Sandboxes"], r.Sandbox.Name+" "+r.Label) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m *model) visiblePlugins() []guestplugins.Choice {
	var rows []guestplugins.Choice
	for _, p := range m.plugins {
		if fuzzy(m.queries["Plugins"], p.ID) {
			if change, ok := m.pendingChoices["plugin:"+p.ID]; ok && change.enabled != nil {
				p.Enabled = *change.enabled
			}
			rows = append(rows, p)
		}
	}
	return rows
}
func (m *model) ensureSelection() {
	rows := m.visibleRows()
	if _, ok := findRow(rows, m.selected); !ok {
		if len(rows) > 0 {
			m.selected = rows[0].Sandbox.ID
		} else {
			m.selected = ""
		}
		if m.focus == "actions" || m.focus == "plugins" {
			m.focus = "table"
		}
	}
	m.expanded = m.selected
	if m.page == "Sandboxes" && (m.focus == "actions" || m.focus == "plugins") {
		if row, ok := m.row(); ok {
			count := len(m.sandboxActions(row))
			m.button = max(0, min(m.button, count-1))
			if m.focus == "actions" && count == 0 {
				m.focus = "table"
				if len(m.plugins) > 0 {
					m.focus = "plugins"
				}
			} else if m.focus == "plugins" && len(m.plugins) == 0 {
				m.focus = "table"
				if count > 0 {
					m.focus = "actions"
				}
			}
		}
	}
	m.detailIndex = max(0, min(m.detailIndex, len(m.plugins)-1))

	plugins := m.visiblePlugins()
	m.pluginIndex = min(m.pluginIndex, max(0, len(plugins)-1))
	if len(plugins) > 0 {
		m.pluginSelected = plugins[m.pluginIndex].ID
	}
}
func (m *model) startWorker() tea.Cmd {
	if !m.workerIntentSet {
		if saved, err := store.ReadStatusStore(); err == nil && saved.WorkerControl != nil {
			m.workerStartGeneration = saved.WorkerControl.Generation
		}
	}
	if m.pending["worker"] {
		return m.setWorkerEnabled(true)
	}
	m.workerEnabled = true
	m.workerCommandEnabled = true
	m.workerControlError = false
	m.stopRequestedAt = time.Time{}
	m.stopDelayReported = false
	start := m.startWorkerFn
	if m.service != nil {
		generation := m.workerStartGeneration
		start = func() error { return m.service.lifecycle.StartWorkerExplicitAtGeneration(generation) }
	}
	return m.run("worker", store.CodeDaemonStart, "Management start requested", start)
}
func (m *model) setEnabled(choice store.EnabledChoice) tea.Cmd {
	r, ok := m.row()
	if !ok || m.pending[r.Sandbox.ID] {
		return nil
	}
	if r.AssociationIssue != "" {
		m.notice = r.AssociationIssue
		return nil
	}
	if choice == store.Enabled && !r.EntryPresent && strings.EqualFold(r.Sandbox.Status, "stopped") {
		return nil
	}
	if m.pendingEnabled == nil {
		m.pendingEnabled = map[string]store.EnabledChoice{}
	}
	m.pendingEnabled[r.Sandbox.ID] = choice
	for i := range m.rows {
		if m.rows[i].Sandbox.ID == r.Sandbox.ID {
			m.rows[i].Enabled = choice
		}
	}
	command := m.run(r.Sandbox.ID, store.CodeManagement, "Sandbox connection choice saved", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return m.setEnabledFn(ctx, r.Scope, r.Sandbox.ID, choice)
	})
	return func() tea.Msg {
		result := command().(operationMsg)
		settled := r.Enabled
		choices, err := store.ReadEnabled()
		if err == nil {
			settled = choices[store.StatusKey(r.Scope, r.Sandbox.ID)]
			if settled == "" {
				settled = store.Enabled
			}
		} else {
			result.err = errors.Join(result.err, fmt.Errorf("cannot confirm saved enabled choice: %w", err))
		}
		result.enabledChoice = &settled
		return result
	}
}
func (m *model) toggleEnabled() tea.Cmd {
	r, ok := m.row()
	if !ok {
		return nil
	}
	// The missing-entry dot always means Enable, including a failed first add.
	if r.EntryPresent && r.Enabled == store.Enabled {
		return m.setEnabled(store.Disabled)
	}
	return m.setEnabled(store.Enabled)
}
func (m *model) removeEntry() tea.Cmd {
	r, ok := m.row()
	if !ok || !canRemoveEntry(r) {
		return nil
	}
	return m.run(r.Sandbox.ID, store.CodeManagement, "Entry removed from herdr; sandbox remains disabled", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return m.removeEntryFn(ctx, r.Scope, r.Sandbox.ID)
	})
}
func (m *model) beginRename() tea.Cmd {
	m.clearNotice()
	r, ok := m.row()
	if !ok {
		return nil
	}
	if !r.EntryPresent || r.AssociationIssue != "" || m.pending[r.Sandbox.ID] {
		m.notice = "Rename requires a registered herdr entry"
		return nil
	}
	m.focus = "table"
	m.reveal = true
	m.input = &inputState{kind: "rename", target: r.Sandbox.ID, text: r.Label, cursor: len([]rune(r.Label))}
	return nil
}

func (m *model) changeOverride(inherit bool) tea.Cmd {
	r, ok := findRow(m.rows, m.expanded)
	if !ok || len(m.plugins) == 0 {
		return nil
	}
	p := m.plugins[min(m.detailIndex, len(m.plugins)-1)]
	var enabled *bool
	if !inherit {
		value, overridden := m.settings.Overrides[store.StatusKey(r.Scope, r.Sandbox.ID)][p.ID]
		if !overridden {
			next := true
			enabled = &next
		} else if value {
			next := false
			enabled = &next
		}
	}
	if p.UnsupportedReason != "" && (enabled != nil && *enabled || enabled == nil && m.settings.Plugins[p.ID].Enabled) {
		m.notice = "Cannot turn on " + p.ID + " for " + r.Sandbox.Name + ": " + p.UnsupportedReason
		return nil
	}
	change := pluginChoiceChange{target: store.StatusKey(r.Scope, r.Sandbox.ID), pluginID: p.ID, enabled: enabled}
	return m.savePluginChoice(r.Sandbox.ID, change, func() error { return store.SetSandboxPluginOverride(r.Scope, r.Sandbox.ID, p.ID, enabled) })
}
func (m *model) savePluginChoice(target string, change pluginChoiceChange, save func() error) tea.Cmd {
	if m.pending[target] {
		return nil
	}
	m.pending[target] = true
	m.pendingChoices[target] = change
	m.actionNotice(target, store.CodePluginChoice)
	return func() tea.Msg {
		return operationMsg{target: target, code: store.CodePluginChoice, notice: "herdr plugin choice saved; installed files are preserved", err: save(), pluginChoice: &change}
	}
}
func (m *model) applyPluginChoice(change pluginChoiceChange) {
	if change.defaultChoice {
		if m.settings.Plugins == nil {
			m.settings.Plugins = map[string]store.GuestPluginDefault{}
		}
		setting := m.settings.Plugins[change.pluginID]
		setting.Enabled = *change.enabled
		m.settings.Plugins[change.pluginID] = setting
		for i := range m.plugins {
			if m.plugins[i].ID == change.pluginID {
				m.plugins[i].Enabled = *change.enabled
			}
		}
		return
	}
	if m.settings.Overrides == nil {
		m.settings.Overrides = map[string]map[string]bool{}
	}
	if m.settings.Overrides[change.target] == nil {
		m.settings.Overrides[change.target] = map[string]bool{}
	}
	if change.enabled == nil {
		delete(m.settings.Overrides[change.target], change.pluginID)
	} else {
		m.settings.Overrides[change.target][change.pluginID] = *change.enabled
	}
}
func (m *model) pluginChoiceDisplay(scope, sandboxID, pluginID string) (string, bool) {
	key := store.StatusKey(scope, sandboxID)
	if pending, ok := m.pendingChoices[sandboxID]; ok && pending.target == key && pending.pluginID == pluginID {
		if pending.enabled == nil {
			return "Inherit", true
		}
		return onOff(*pending.enabled), true
	}
	if value, ok := m.settings.Overrides[key][pluginID]; ok {
		return onOff(value), false
	}
	return "Inherit", false
}
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}

// SSH setup is the sole terminal handoff. Ordinary operations stay asynchronous.
type sshCommand struct{ service *Service }

func (c sshCommand) Run() error        { err, _ := c.service.runSSHSetup(); return err }
func (sshCommand) SetStdin(io.Reader)  {}
func (sshCommand) SetStdout(io.Writer) {}
func (sshCommand) SetStderr(io.Writer) {}

// A stopped worker must not prevent recovery of access left by an interrupted
// installation. Request this once per opening as soon as the daemon is known.
