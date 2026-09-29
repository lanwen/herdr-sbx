package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var lifecycleReader = readLifecycle

var lifecycleCatchupInterval = 30 * time.Second

func watch(out io.Writer) error {
	out = &lockedWriter{out: out}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var scheduler *lifecycleScheduler

	for {
		p := inspect()
		printChecks(out, p)
		if p.Scope == "" || p.Socket == "" || !hasLevel(p.Checks, "Sandbox capability", ready) || !hasLevel(p.Checks, "Sandbox daemon", ready) || !hasLevel(p.Checks, "Sandbox inventory", ready) || hasBlocked(p.Checks, "Docker authorization") {
			return errors.New("watch needs a running sandbox daemon and readable sandbox inventory")
		}

		fmt.Fprintln(out, "Watching sandbox lifecycle. Press Ctrl-C to stop.")
		if scheduler == nil {
			scheduler = newLifecycleScheduler(ctx, p.Scope, p.Socket, out)
		} else if scheduler.scope != p.Scope || scheduler.socket != p.Socket {
			stop()
			scheduler.wait()
			return errors.New("sandbox daemon scope changed; restart the lifecycle watch")
		}
		scheduler.beginStream()
		err := lifecycleReader(ctx, p.Socket, scheduler.enqueue)
		if ctx.Err() != nil {
			scheduler.wait()
			fmt.Fprintln(out, "Lifecycle watch stopped.")
			return nil
		}
		if err != nil {
			fmt.Fprintln(out, "Lifecycle stream ended:", err)
		} else {
			fmt.Fprintln(out, "Lifecycle stream ended; reconnecting.")
		}
		select {
		case <-ctx.Done():
			scheduler.wait()
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
	ctx     context.Context
	scope   string
	socket  string
	out     io.Writer
	mu      sync.Mutex
	retired map[string]bool
	deletes map[string]lifecycleEvent
	lastSeq *uint64
	catchup bool
	queue   chan lifecycleEvent
	jobs    sync.WaitGroup
}

func newLifecycleScheduler(ctx context.Context, scope, socket string, out io.Writer) *lifecycleScheduler {
	s := &lifecycleScheduler{ctx: ctx, scope: scope, socket: socket, out: out, retired: map[string]bool{}, deletes: map[string]lifecycleEvent{}, queue: make(chan lifecycleEvent, 128)}
	s.jobs.Add(1)
	go s.run()
	return s
}

func (s *lifecycleScheduler) enqueue(event lifecycleEvent) bool {
	if event.Type != "sandbox.lifecycle" {
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
	s.mu.Unlock()
}

func (s *lifecycleScheduler) run() {
	defer s.jobs.Done()
	deleteRetry := time.NewTicker(time.Second)
	defer deleteRetry.Stop()
	catchup := time.NewTicker(lifecycleCatchupInterval)
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
		case <-catchup.C:
			s.processCatchup(true)
		}
	}
}

func (s *lifecycleScheduler) processCatchup(periodic bool) {
	s.mu.Lock()
	needed := s.catchup
	s.catchup = false
	s.mu.Unlock()
	if !periodic && !needed {
		return
	}
	if err := reconcileContext(s.ctx, s.out); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(s.out, "Lifecycle catch-up failed:", err)
	}
}

func (s *lifecycleScheduler) wait() {
	s.jobs.Wait()
}

func (s *lifecycleScheduler) handle(event lifecycleEvent) {
	switch strings.ToLower(event.Action) {
	case "sync", "complete":
		fmt.Fprintln(s.out, "Initial sandbox lifecycle state received.")
	case "started", "created":
		if !s.current(event) {
			return
		}
		if err := reconcileMatching(s.ctx, s.out, func(sb sandbox) bool {
			return sb.ID == event.SandboxID && sb.Name == event.SandboxName
		}, func(sandbox) bool { return s.current(event) }); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(s.out, "Lifecycle registration failed:", err)
		}
		if s.isRetired(event.SandboxID) {
			s.removeDeleted(event)
		}
	case "stopped":
		fmt.Fprintf(s.out, "%s stopped; its registration is retained for SSH reconnects.\n", event.SandboxName)
	case "deleted":
		s.removeDeleted(event)
	}
}

func (s *lifecycleScheduler) isRetired(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retired[id]
}

func (s *lifecycleScheduler) stillRunning(event lifecycleEvent) bool {
	return sandboxRunning(s.socket, sandbox{ID: event.SandboxID, Name: event.SandboxName})
}

func (s *lifecycleScheduler) current(event lifecycleEvent) bool {
	return !s.isRetired(event.SandboxID) && event.SandboxName != "" && s.stillRunning(event)
}

func (s *lifecycleScheduler) processDeletes() {
	s.mu.Lock()
	deletes := make([]lifecycleEvent, 0, len(s.deletes))
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

func (s *lifecycleScheduler) removeDeleted(event lifecycleEvent) bool {
	if event.SandboxID == "" {
		return true
	}
	unlock, err := lockState()
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred:", err)
		return false
	}
	defer unlock()

	st, err := loadState()
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred:", err)
		return false
	}
	var item *tracked
	for i := range st.Scopes[s.scope] {
		if st.Scopes[s.scope][i].SandboxID == event.SandboxID {
			item = &st.Scopes[s.scope][i]
			break
		}
	}
	if item == nil {
		return true
	}
	if item.ProfileID == "" || item.Pending {
		fmt.Fprintln(s.out, "Lifecycle deletion retained an unresolved registration intent; inspect the Herdr machine catalog before retrying.")
		return true
	}

	list, err := runShortContext(s.ctx, herdrBinary(), "machine", "list", "--json")
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred: cannot inspect Herdr machines:", commandError(err, list))
		return false
	}
	machines, err := parseMachines(list)
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred:", err)
		return false
	}
	if machineByID(machines, item.ProfileID) == nil {
		return s.clearDeleted(&st, event.SandboxID, item.Label, "machine was already absent")
	}
	if ownedMachineByID(machines, item.ProfileID, item.Label, item.Target) == nil {
		fmt.Fprintln(s.out, "Lifecycle deletion retained state: saved machine is no longer the owned profile.")
		return true
	}
	removed, err := runBoundedContext(s.ctx, 15*time.Second, herdrBinary(), "machine", "remove", item.ProfileID)
	if err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion deferred:", commandError(err, removed))
		return false
	}
	return s.clearDeleted(&st, event.SandboxID, item.Label, "removed its owned Herdr machine")
}

func (s *lifecycleScheduler) clearDeleted(st *state, sandboxID, label, detail string) bool {
	if err := removeTracked(st, s.scope, sandboxID); err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion retained state:", err)
		return false
	}
	if err := saveState(*st); err != nil {
		fmt.Fprintln(s.out, "Lifecycle deletion retained state:", err)
		return false
	}
	fmt.Fprintf(s.out, "%s deleted; %s.\n", label, detail)
	return true
}
