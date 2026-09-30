package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type lifecycleFixture struct {
	t       *testing.T
	service *Service
	scope   string
	catalog string
	removed []string
	run     func(context.Context, process.Command) (string, error)
	request lifecycleTransport
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_PLUGIN_ID", "herdr.docker-sandboxes")
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sbx", "ssh"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	f := &lifecycleFixture{t: t, scope: store.ScopeID("/tmp/lifecycle-test.sock"), catalog: `[{"profile_id":"machine-1","label":"sbx · demo","ssh_target":"demo.sbx","remote_session":"default"}]`}
	f.run = f.defaultRun
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		if r.URL.Path == "/sandbox/demo" {
			return lifecycleResponse(404, `{"code":"sandbox_not_found"}`), nil
		}
		return nil, fmt.Errorf("unexpected request: %s", r.URL)
	}
	runner := process.Runner{Execute: func(ctx context.Context, cmd process.Command) (string, error) { return f.run(ctx, cmd) }}
	sandboxClient := sbx.NewWithTransport(runner, lifecycleTransport(func(r *http.Request) (*http.Response, error) { return f.request(r) }))
	herdrClient := herdr.New(runner)
	inspector := precheck.New(sandboxClient, herdrClient, runner)
	registrations := registration.New(sandboxClient, herdrClient, inspector, runner)
	f.service = New(registrations, sandboxClient, herdrClient)
	return f
}

func lifecycleResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func (f *lifecycleFixture) defaultRun(_ context.Context, cmd process.Command) (string, error) {
	call := cmd.Name + " " + strings.Join(cmd.Args, " ")
	switch call {
	case "herdr --version":
		return "herdr 0.9.1", nil
	case "sbx --version":
		return "sbx 0.45.1", nil
	case "ssh -V":
		return "OpenSSH", nil
	case "sbx daemon status --help", "sbx ls --help":
		return "--json", nil
	case "sbx daemon status --json":
		return `{"status":"running","socket":"/tmp/lifecycle-test.sock"}`, nil
	case "sbx diagnose --json":
		return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
	case "ssh -G herdr-sandbox-probe.sbx":
		return "proxycommand sbx ssh proxy %n", nil
	case "herdr machine list --json":
		return f.catalog, nil
	case "herdr machine remove machine-1":
		f.removed = append(f.removed, "machine-1")
		return "", nil
	}
	if len(cmd.Args) > 0 && cmd.Name == "herdr" && cmd.Args[0] == "notification" {
		return "", nil
	}
	return "", fmt.Errorf("unexpected command: %s", call)
}

func (f *lifecycleFixture) save(items ...store.Tracked) {
	f.t.Helper()
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: items}}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *lifecycleFixture) scheduler() *lifecycleScheduler {
	return &lifecycleScheduler{owner: f.service, ctx: context.Background(), scope: f.scope, socket: "/tmp/lifecycle-test.sock", out: io.Discard, retired: map[string]bool{}, deletes: map[string]sbx.LifecycleEvent{}, queue: make(chan sbx.LifecycleEvent, 2)}
}

func ownedRegistration() store.Tracked {
	return store.Tracked{SandboxID: "old", Label: "sbx · demo", Target: "demo.sbx", ProfileID: "machine-1"}
}

func (f *lifecycleFixture) assertCount(want int) {
	f.t.Helper()
	state, err := store.LoadState()
	if err != nil || len(state.Scopes[f.scope]) != want {
		f.t.Fatalf("state=%#v, err=%v, want %d registrations", state, err, want)
	}
}

func TestLifecycleDeletionPreservesStateWhenAbsenceIsUnknown(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.request = func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") }
	if f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}) {
		t.Fatal("unknown absence was treated as completed deletion")
	}
	f.assertCount(1)
}

func TestCatchupRecoversConfirmedDeletionWhenInventoryIsAmbiguous(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.scheduler().processCatchup(true)
	f.assertCount(0)
	if len(f.removed) != 1 {
		t.Fatalf("removed = %v", f.removed)
	}
}

func TestLifecycleSchedulerIgnoresOtherEventTypes(t *testing.T) {
	s := newLifecycleFixture(t).scheduler()
	if !s.enqueue(sbx.LifecycleEvent{Type: "other", Action: "deleted", SandboxID: "old"}) || len(s.retired) != 0 || len(s.deletes) != 0 || len(s.queue) != 0 {
		t.Fatal("unrelated event changed scheduler state")
	}
}

func TestLifecycleDeletionRemovesOnlyOwnedProfileInItsScope(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: {item}, "other-scope": {item}}}); err != nil {
		t.Fatal(err)
	}
	if !f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}) || len(f.removed) != 1 {
		t.Fatalf("owned deletion did not finish: %v", f.removed)
	}
	f.assertCount(0)
	state, _ := store.LoadState()
	if len(state.Scopes["other-scope"]) != 1 {
		t.Fatal("deletion changed another daemon scope")
	}
}

func TestLifecycleDeletionRetainsMismatchedProfile(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.catalog = `[{"profile_id":"machine-1","label":"manual","ssh_target":"different","remote_session":"default"}]`
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(1)
	if len(f.removed) != 0 {
		t.Fatal("manually changed profile removed")
	}
}

func TestLifecycleQueueKeepsDeletionWhenFull(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	s := f.scheduler()
	for len(s.queue) < cap(s.queue) {
		s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "queued"})
	}
	if s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "old"}) {
		t.Fatal("full queue accepted event")
	}
	if !s.retired["old"] || len(s.deletes) != 1 {
		t.Fatal("overflow lost deletion intent")
	}
	s.processDeletes()
	f.assertCount(0)
	if len(s.deletes) != 0 || len(f.removed) != 1 {
		t.Fatal("overflow deletion was not processed")
	}
}

func TestLifecycleGapRequestsCatchupWithoutCrossStreamComparison(t *testing.T) {
	s := newLifecycleFixture(t).scheduler()
	for _, seq := range []uint64{3, 5} {
		s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "old", Seq: &seq})
	}
	if !s.catchup {
		t.Fatal("sequence gap did not request catch-up")
	}
	s.catchup = false
	s.beginStream()
	seq := uint64(100)
	s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "old", Seq: &seq})
	if s.catchup {
		t.Fatal("new stream compared its sequence to the previous stream")
	}
}

func TestLifecycleDeletionKeepsPendingIntentVisible(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	item.Pending, item.ProfileID = true, ""
	f.save(item)
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(1)
	if len(f.removed) != 0 {
		t.Fatal("unresolved profile removed")
	}
}

func TestLifecycleDeletionClearsStateWhenOwnedProfileIsAlreadyAbsent(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.catalog = `[]`
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(0)
	if len(f.removed) != 0 {
		t.Fatal("absent machine was removed twice")
	}
}

func TestWorkerUsesPluginAndSessionLiveness(t *testing.T) {
	f := newLifecycleFixture(t)
	enabled := true
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		switch strings.Join(cmd.Args, " ") {
		case "plugin list --plugin herdr.docker-sandboxes --json":
			return fmt.Sprintf(`[{"id":"herdr.docker-sandboxes","enabled":%t}]`, enabled), nil
		case "status server --json":
			return `{"running":false}`, nil
		case "session list --json":
			return `{"sessions":[{"running":true,"socket_path":"new.sock"}]}`, nil
		}
		return "", errors.New("unexpected command")
	}
	if !f.service.workerAlive() {
		t.Fatal("running participating session did not keep worker alive")
	}
	enabled = false
	if f.service.workerAlive() {
		t.Fatal("disabled plugin kept worker alive")
	}
}

func TestWorkerRebindsToTheOtherRunningSession(t *testing.T) {
	f := newLifecycleFixture(t)
	f.service.herdr.RebindSocket("old.sock")
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "session list --json" {
			if len(cmd.Unset) == 0 {
				t.Fatal("session discovery inherited worker socket")
			}
			return `{"sessions":[{"running":false,"socket_path":"old.sock"},{"running":true,"socket_path":"new.sock"}]}`, nil
		}
		if strings.Join(cmd.Args, " ") != "status server --json" || cmd.Env["HERDR_SOCKET_PATH"] != "new.sock" {
			t.Fatalf("status was not rebound: %#v", cmd)
		}
		return `{"running":true}`, nil
	}
	if !f.service.herdr.RebindToRunningSession(context.Background()) || !f.service.herdr.LocalServerRunning(context.Background()) {
		t.Fatal("failed to bind the new server")
	}
}

func TestWorkerAllowsABriefServerHandoffGap(t *testing.T) {
	f := newLifecycleFixture(t)
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		switch cmd.Args[0] {
		case "plugin":
			return `[{"id":"herdr.docker-sandboxes","enabled":true}]`, nil
		case "status":
			return `{"running":false}`, nil
		case "session":
			return `{"sessions":[]}`, nil
		}
		return "", errors.New("unexpected command")
	}
	f.service.workerPoll, f.service.serverHandoff = time.Millisecond, 30*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	started := time.Now()
	go func() { f.service.stopWhenServerEnds(ctx, cancel); close(done) }()
	select {
	case <-done:
		if time.Since(started) < f.service.serverHandoff {
			t.Fatal("worker stopped before its handoff grace period")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop after its handoff grace period")
	}
}

func TestWorkerReconnectsWhileHerdrRemainsLive(t *testing.T) {
	f := newLifecycleFixture(t)
	var attempts atomic.Int32
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		switch strings.Join(cmd.Args, " ") {
		case "plugin list --plugin herdr.docker-sandboxes --json":
			return fmt.Sprintf(`[{"id":"herdr.docker-sandboxes","enabled":%t}]`, attempts.Load() < 2), nil
		case "status server --json":
			return `{"running":true}`, nil
		case "daemon status --json":
			attempts.Add(1)
			return `{"status":"stopped","socket":"/tmp/lifecycle-test.sock"}`, nil
		}
		return f.defaultRun(ctx, cmd)
	}
	f.service.workerReconnect = time.Millisecond
	if err := f.service.Worker(io.Discard); err != nil || attempts.Load() != 2 {
		t.Fatalf("attempts=%d, error=%v", attempts.Load(), err)
	}
}

func TestLocalServerStatusUsesSupportedJSON(t *testing.T) {
	f := newLifecycleFixture(t)
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		if cmd.Name != "herdr" || strings.Join(cmd.Args, " ") != "status server --json" {
			t.Fatalf("unexpected server query: %#v", cmd)
		}
		return `{"running":true}`, nil
	}
	if !f.service.herdr.LocalServerRunning(context.Background()) {
		t.Fatal("server was not recognized as running")
	}
}

func TestWatchCancelsSchedulerBeforeAWorkerRetry(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{})
	canceled := make(chan error, 1)
	var streamEnded atomic.Bool
	f.run = func(commandContext context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "--machine machine-1 status server" {
			close(entered)
			<-commandContext.Done()
			canceled <- commandContext.Err()
			return "", commandContext.Err()
		}
		if cmd.Name == "sbx" && strings.Join(cmd.Args, " ") == "daemon status --json" && streamEnded.Load() {
			return `{"status":"stopped","socket":"/tmp/lifecycle-test.sock"}`, nil
		}
		return f.defaultRun(commandContext, cmd)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/events" {
			return &http.Response{StatusCode: 200, Body: reader}, nil
		}
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[{"id":"old","name":"demo","status":"running"}]`), nil
		}
		return nil, fmt.Errorf("unexpected request: %s", r.URL)
	}
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		fmt.Fprintln(writer, `{"type":"sandbox.lifecycle","action":"started","sandbox_id":"old","sandbox_name":"demo"}`)
		select {
		case <-entered:
			streamEnded.Store(true)
		case <-ctx.Done():
		}
		writer.Close()
	}()
	err := f.service.watchContext(ctx, io.Discard)
	<-producerDone
	if err == nil || ctx.Err() != nil {
		t.Fatalf("watch did not stop on lost daemon: err=%v context=%v", err, ctx.Err())
	}
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("scheduler command ended with %v", err)
		}
	default:
		t.Fatal("watch returned before its in-flight scheduler command was canceled")
	}
}
