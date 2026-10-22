package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func (svc *Service) Watch(out io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return svc.watchContext(ctx, out)
}

func (svc *Service) watchContext(ctx context.Context, out io.Writer) error {
	return svc.watchWithStatus(ctx, out, nil)
}

func (svc *Service) watchWithStatus(ctx context.Context, out io.Writer, report func(string, string, bool)) error {
	baseline := managementBaseline{}
	return svc.watchManaged(ctx, out, report, &baseline)
}

type managementBaseline struct{ scope, socket string }

// watchManaged retains the ON baseline across event-stream reconnects. A new
// management period receives a new baseline only after the old worker settles.
func (svc *Service) watchManaged(ctx context.Context, out io.Writer, report func(string, string, bool), baseline *managementBaseline) error {
	out = &lockedWriter{out: out}
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	var scheduler *lifecycleScheduler
	defer func() {
		stopScheduler()
		if scheduler != nil {
			scheduler.wait()
		}
	}()

	for {
		if !automaticAllowed(ctx) {
			return nil
		}
		p := svc.registrations.Inspect()
		svc.checks(out, p)
		if p.Scope == "" || p.Socket == "" || !hasLevel(p, "Sandbox capability", precheck.Ready) || !hasLevel(p, "Sandbox daemon", precheck.Ready) || hasBlocked(p, "Docker authorization") {
			if scheduler != nil {
				if err := store.MarkScopeStale(scheduler.scope, "sandbox discovery is unavailable"); err != nil {
					fmt.Fprintln(out, "Sandbox status was not saved:", err)
				}
				svc.warnBlocking(ctx, out, scheduler.scope+"/discovery", "Sandbox discovery is unavailable. Check Docker Sandboxes from the status panel.")
			}
			return errors.New("watch needs a running sandbox daemon and readable sandbox inventory")
		}
		if scheduler != nil && (scheduler.scope != p.Scope || scheduler.socket != p.Socket) {
			return errors.New("sandbox daemon scope changed; restart the lifecycle watch")
		}
		if baseline.scope != p.Scope || baseline.socket != p.Socket {
			if err := svc.registrations.CaptureBaseline(ctx, p.Socket); err != nil {
				return fmt.Errorf("read management baseline before automatic work: %w", err)
			}
			*baseline = managementBaseline{scope: p.Scope, socket: p.Socket}
		}
		if !automaticAllowed(ctx) {
			return nil
		}
		if err := svc.registrations.RecoverPendingPolicy(p.Scope, p.Socket); err != nil {
			return err
		}
		if err := store.ClearWarning(p.Scope + "/discovery"); err != nil {
			fmt.Fprintln(out, "Sandbox warning state was not saved:", err)
		}

		fmt.Fprintln(out, "Watching sandbox lifecycle. Press Ctrl-C to stop.")
		if scheduler == nil {
			scheduler = svc.newScheduler(schedulerCtx, p.Scope, p.Socket, out, report)
		}
		scheduler.beginStream()
		streamReported := false
		err := svc.sbx.ReadLifecycle(ctx, p.Socket, func(event sbx.LifecycleEvent) bool {
			if report != nil && !streamReported && (event.Type == "sandbox.lifecycle" || event.Type == "sync") {
				streamReported = true
				report("running", "Following sandbox lifecycle events.", false)
			}
			return scheduler.enqueue(event)
		})
		if ctx.Err() != nil {
			fmt.Fprintln(out, "Lifecycle watch stopped.")
			return nil
		}
		if !automaticAllowed(ctx) {
			return nil
		}
		if report != nil {
			detail := "Sandbox event stream ended; reconnecting."
			if err != nil {
				detail = err.Error()
			}
			report("reconnecting", detail, false)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			if err := store.MarkScopeStale(scheduler.scope, "sandbox lifecycle stream interrupted"); err != nil {
				fmt.Fprintln(out, "Sandbox status was not saved:", err)
			}
			svc.warnBlocking(ctx, out, scheduler.scope+"/stream", "Sandbox lifecycle stream interrupted. The worker will reconnect.")
			fmt.Fprintln(out, "Lifecycle stream ended:", err)
		} else {
			if err := store.MarkScopeStale(scheduler.scope, "sandbox lifecycle stream interrupted"); err != nil {
				fmt.Fprintln(out, "Sandbox status was not saved:", err)
			}
			svc.warnBlocking(ctx, out, scheduler.scope+"/stream", "Sandbox lifecycle stream interrupted. The worker will reconnect.")
			fmt.Fprintln(out, "Lifecycle stream ended; reconnecting.")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}

type lockedWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

type lifecycleScheduler struct {
	enabled        map[string]store.EnabledChoice
	pluginDefaults store.GuestPluginDefaults
	report         func(string, string, bool)
	owner          *Service
	ctx            context.Context
	scope          string
	socket         string
	out            io.Writer
	mu             sync.Mutex
	retired        map[string]bool
	deletes        map[string]sbx.LifecycleEvent
	lastSeq        *uint64
	catchup        bool
	streamStarted  bool
	freshCatchup   bool
	queue          chan sbx.LifecycleEvent
	jobs           sync.WaitGroup
}

func (owner *Service) newScheduler(ctx context.Context, scope, socket string, out io.Writer, report func(string, string, bool)) *lifecycleScheduler {
	s := &lifecycleScheduler{report: report, owner: owner, ctx: ctx, scope: scope, socket: socket, out: out, retired: map[string]bool{}, deletes: map[string]sbx.LifecycleEvent{}, queue: make(chan sbx.LifecycleEvent, 128)}
	s.pluginDefaults, _ = store.ReadGuestPluginDefaults()
	s.enabled, _ = store.ReadEnabled()
	s.jobs.Add(1)
	go s.run()
	return s
}

func (s *lifecycleScheduler) enqueue(event sbx.LifecycleEvent) bool {
	if event.Type != "sandbox.lifecycle" && !(event.Type == "sync" && (strings.EqualFold(event.Action, "sync") || strings.EqualFold(event.Action, "complete"))) {
		return true
	}
	action := strings.ToLower(event.Action)
	if action == "" || (action != "sync" && action != "complete" && event.SandboxID == "") {
		return true
	}
	s.mu.Lock()
	if event.Seq != nil {
		if s.lastSeq != nil && *event.Seq > *s.lastSeq && *event.Seq-*s.lastSeq > 1 {
			s.catchup = true
			s.freshCatchup = true
		}
		if s.lastSeq == nil || *event.Seq > *s.lastSeq {
			seq := *event.Seq
			s.lastSeq = &seq
		}
	}
	if action == "deleted" {
		s.retired[event.SandboxID] = true
		s.deletes[event.SandboxID] = event
	}
	select {
	case s.queue <- event:
		s.mu.Unlock()
		return true
	default:
		s.mu.Unlock()
		return false
	}
}

func (s *lifecycleScheduler) beginStream() {
	s.mu.Lock()
	s.lastSeq = nil
	if s.streamStarted {
		s.catchup = true
		s.freshCatchup = true
	}
	s.streamStarted = true
	s.mu.Unlock()
}

func (s *lifecycleScheduler) run() {
	defer s.jobs.Done()
	deleteRetry := time.NewTicker(time.Second)
	defer deleteRetry.Stop()
	catchup := time.NewTicker(s.owner.catchupInterval)
	defer catchup.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.queue:
			s.handle(event)
			s.processDeletes()
			s.processCatchup(false)
		case <-deleteRetry.C:
			s.processDeletes()
			s.processPluginDefaults()
		case <-catchup.C:
			s.processCatchup(true)
		}
	}
}

// Settings have a separate, atomically replaced file. Polling it also works
// across CLI processes and saves made while the worker was not running.
func (s *lifecycleScheduler) processPluginDefaults() {
	if !automaticAllowed(s.ctx) {
		return
	}
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return
	} // Periodic reconciliation reports unreadable defaults.
	enabled, err := store.ReadEnabled()
	if err != nil {
		return
	}
	if reflect.DeepEqual(settings, s.pluginDefaults) && maps.Equal(enabled, s.enabled) {
		return
	}
	s.pluginDefaults = settings
	s.enabled = enabled
	s.processCatchup(true)
}

func (s *lifecycleScheduler) processCatchup(periodic bool) {
	if !automaticAllowed(s.ctx) {
		return
	}
	s.mu.Lock()
	needed := s.catchup
	fresh := s.freshCatchup
	s.catchup = false
	if periodic || needed {
		s.freshCatchup = false
	}
	s.mu.Unlock()
	if !periodic && !needed {
		return
	}
	ctx := s.ctx
	if fresh {
		ctx = registration.WithFreshGuestObservation(ctx)
	}
	if err := s.owner.registrations.ReconcileContext(ctx, s.out); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if fresh {
			s.mu.Lock()
			s.freshCatchup = true
			s.mu.Unlock()
		}
		if !errors.Is(err, context.Canceled) {
			detail := fmt.Sprintf("Could not refresh sandbox state: %v", err)
			if statusErr := store.MarkScopeStale(s.scope, detail); statusErr != nil {
				fmt.Fprintln(s.out, "Sandbox status was not saved:", statusErr)
			}
			s.owner.warnBlocking(s.ctx, s.out, s.scope+"/catch-up", detail)
			fmt.Fprintln(s.out, "Lifecycle catch-up failed:", err)
		}
	} else {
		if s.report != nil {
			s.report("", "", true)
		}
		if err := store.ClearWarning(s.scope + "/catch-up"); err != nil {
			fmt.Fprintln(s.out, "Sandbox warning state was not saved:", err)
		}
	}
	s.recoverMissedDeletes()
}

func (s *lifecycleScheduler) wait() {
	s.jobs.Wait()
}

func (s *lifecycleScheduler) handle(event sbx.LifecycleEvent) {
	if !automaticAllowed(s.ctx) {
		return
	}
	switch strings.ToLower(event.Action) {
	case "sync", "complete":
		if s.report != nil {
			s.report("", "", true)
		}
		if err := store.ClearWarning(s.scope + "/stream"); err != nil {
			fmt.Fprintln(s.out, "Sandbox warning state was not saved:", err)
		}
		fmt.Fprintln(s.out, "Initial sandbox lifecycle state received.")
	case "started", "created":
		if !s.current(event) {
			return
		}
		if err := s.owner.registrations.ReconcileSandbox(registration.WithFreshGuestObservation(s.ctx), s.out, event.SandboxID, event.SandboxName, func() bool { return s.current(event) }); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(s.out, "Lifecycle registration failed:", err)
		}
		if s.isRetired(event.SandboxID) {
			s.removeDeleted(event)
		}
	case "stopped":
		// A stopped event is an observation, not a no-wake guarantee: an
		// independently enabled herdr reconnect may already be in flight.
		fmt.Fprintf(s.out, "%s stopped; deferring guest work.\n", event.SandboxName)
		s.processCatchup(true)
	case "deleted":
		s.removeDeleted(event)
	}
}

func (s *lifecycleScheduler) isRetired(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retired[id]
}

func (s *lifecycleScheduler) stillRunning(event sbx.LifecycleEvent) bool {
	return s.owner.registrations.SandboxRunning(s.socket, sbx.Sandbox{ID: event.SandboxID, Name: event.SandboxName})
}

func (s *lifecycleScheduler) current(event sbx.LifecycleEvent) bool {
	return automaticAllowed(s.ctx) && !s.isRetired(event.SandboxID) && event.SandboxName != "" && s.stillRunning(event)
}

func (s *lifecycleScheduler) processDeletes() {
	if !automaticAllowed(s.ctx) {
		return
	}
	s.mu.Lock()
	deletes := make([]sbx.LifecycleEvent, 0, len(s.deletes))
	for _, event := range s.deletes {
		deletes = append(deletes, event)
	}
	s.mu.Unlock()
	for _, event := range deletes {
		if s.removeDeleted(event) {
			s.mu.Lock()
			delete(s.deletes, event.SandboxID)
			s.mu.Unlock()
		}
	}
}

func (s *lifecycleScheduler) recoverMissedDeletes() {
	if !automaticAllowed(s.ctx) {
		return
	}
	sandboxes, err := s.owner.sbx.Inventory(s.ctx, s.socket)
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion check deferred:", err)
		return
	}
	present := make(map[string]bool, len(sandboxes))
	for _, sandbox := range sandboxes {
		present[sandbox.ID] = true
	}
	st, err := store.LoadState()
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle recovery deferred:", err)
		return
	}
	// An earlier daemon broadcast this warning to every sandbox in the scope.
	// Successful inventory now limits uncertainty to missing tracked identities.
	if err := store.UpdateStatusStore(func(status *store.StatusStore) {
		delete(status.Warnings, s.scope+"/absence")
		for id := range present {
			key := store.StatusKey(s.scope, id)
			if cached, ok := status.Sandboxes[key]; ok {
				cached.ConfirmPresent()
				status.Sandboxes[key] = cached
			}
			status.SetSandboxDeletionFailure(key, "")
		}
	}); err != nil {
		fmt.Fprintln(s.out, "Sandbox status was not saved:", err)
	}
	for _, item := range st.Scopes[s.scope] {
		if !automaticAllowed(s.ctx) {
			return
		}
		if registration.SandboxName(item) == "" || item.Pending {
			continue
		}
		warningKey := store.StatusKey(s.scope, item.SandboxID) + "/absence"
		if present[item.SandboxID] {
			continue
		}
		if item.ProfileID == "" {
			// Registration verifies the dead identity before clearing local-only state.
			// Its no-profile path also handles a verified same-name replacement safely.
			s.removeDeleted(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: item.SandboxID, SandboxName: registration.SandboxName(item)})
			continue
		}
		absent, err := s.owner.sbx.SandboxAbsent(s.ctx, s.socket, registration.SandboxName(item), item.SandboxID)
		if err != nil {
			if statusErr := store.UpdateStatusStore(func(status *store.StatusStore) {
				key := store.StatusKey(s.scope, item.SandboxID)
				cached := status.Sandboxes[key]
				cached.Scope, cached.SandboxID, cached.Name = s.scope, item.SandboxID, registration.SandboxName(item)
				cached.Stale, cached.Detail = true, store.DeletionUnconfirmedDetail
				status.Sandboxes[key] = cached
				status.SetSandboxDeletionFailure(key, store.DeletionUnconfirmedDetail+" Verification failed: "+err.Error())
			}); statusErr != nil {
				fmt.Fprintln(s.out, "Sandbox status was not saved:", statusErr)
			}
			s.owner.warnBlocking(s.ctx, s.out, warningKey, registration.SandboxName(item)+": "+store.DeletionUnconfirmedDetail)
			fmt.Fprintln(s.out, "Sandbox deletion check deferred:", err)
			continue
		}
		if err := store.UpdateStatusStore(func(status *store.StatusStore) {
			key := store.StatusKey(s.scope, item.SandboxID)
			status.SetSandboxDeletionFailure(key, "")
			if cached, ok := status.Sandboxes[key]; ok && !absent {
				cached.ConfirmPresent()
				status.Sandboxes[key] = cached
			}
		}); err != nil {
			fmt.Fprintln(s.out, "Sandbox status was not saved:", err)
		}
		if absent {
			s.removeDeleted(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: item.SandboxID, SandboxName: registration.SandboxName(item)})
		}
	}
}

func (s *lifecycleScheduler) removeDeleted(event sbx.LifecycleEvent) bool {
	if !automaticAllowed(s.ctx) {
		return false
	}
	result, err := s.owner.registrations.RemoveDeletedSandbox(s.ctx, s.scope, s.socket, event.SandboxID)
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred:", err)
		return false
	}
	if result.Detail != "" {
		fmt.Fprintln(s.out, result.Detail)
	}
	return !result.Retry
}

// Unreadable permission fails closed. Host observations and explicit actions
// use their own paths; this gate is only for the automatic lifecycle worker.
func automaticAllowed(ctx context.Context) bool {
	return registration.CheckAutomaticWork(ctx) == nil
}
