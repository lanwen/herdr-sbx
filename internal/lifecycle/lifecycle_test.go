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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Setenv("HERDR_PLUGIN_ID", "lanwen.herdr-sbx")
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sbx", "ssh"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0700))
	}
	t.Setenv("PATH", bin)
	f := &lifecycleFixture{t: t, scope: store.ScopeID("/tmp/lifecycle-test.sock"), catalog: `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`}
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
	case "sbx version":
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
		f.catalog = `[]`
		return "", nil
	}
	if len(cmd.Args) > 0 && cmd.Name == "herdr" && cmd.Args[0] == "notification" {
		return "", nil
	}
	return "", fmt.Errorf("unexpected command: %s", call)
}

func (f *lifecycleFixture) save(items ...store.Tracked) {
	f.t.Helper()
	require.NoError(f.t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: items}}))
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
	require.NoError(f.t, err)
	require.Len(f.t, state.Scopes[f.scope], want)
}

func TestLifecycleDeletionPreservesStateWhenAbsenceIsUnknown(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.request = func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") }
	require.False(t, f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}), "unknown absence was treated as completed deletion")
	f.assertCount(1)
}

func TestDeletionRecoveryOnlyChecksMissingTrackedSandboxes(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	require.NoError(t, store.RecordSandboxStatus(f.scope, sbx.Sandbox{ID: "old", Name: "demo", Status: "stopped"}, "ready", "retained registration", false))
	f.request = func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/sandbox", r.URL.Path, "probed deletion for present stopped sandbox: %s", r.URL.Path)
		return lifecycleResponse(200, `[{"uuid":"old","name":"demo","status":"stopped"}]`), nil
	}
	f.scheduler().recoverMissedDeletes()
	f.assertCount(1)
}

func TestDeletionRecoveryClearsUnrelatedEarlierDetailWithoutRegistration(t *testing.T) {
	f := newLifecycleFixture(t)
	require.NoError(t, store.RecordSandboxStatus(f.scope, sbx.Sandbox{ID: "new", Name: "new", Status: "stopped"}, "not checked", "sandbox deletion recovery is unavailable", true))
	{
		_, err := store.WarningTransition(f.scope+"/absence", "old warning")
		require.NoError(t, err)
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/sandbox", r.URL.Path, "probed deletion without tracked registration: %s", r.URL.Path)
		return lifecycleResponse(200, `[{"uuid":"new","name":"new","status":"stopped"}]`), nil
	}
	f.scheduler().recoverMissedDeletes()
	status, err := store.ReadStatusStore()
	require.NoError(t, err)

	got := status.Sandboxes[store.StatusKey(f.scope, "new")]
	require.False(t, got.Stale)
	require.EqualValues(t, "", got.Detail)
	require.Len(t, status.Warnings, 0)
}

func TestDeletionRecoveryFailureIsScopedAndClearsAfterRecovery(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	for _, sb := range []sbx.Sandbox{{ID: "old", Name: "demo", Status: "running"}, {ID: "new", Name: "new", Status: "stopped"}} {
		require.NoError(t, store.RecordSandboxStatus(f.scope, sb, "not checked", "awaiting registration", false))
	}
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[{"uuid":"new","name":"new","status":"stopped"}]`), nil
		}
		return nil, errors.New("unavailable")
	}
	f.scheduler().recoverMissedDeletes()
	f.assertCount(1)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	{
		got := status.Sandboxes[store.StatusKey(f.scope, "new")]
		require.False(t, got.Stale)
		require.EqualValues(t, "awaiting registration", got.Detail)
	}
	{
		got := status.Sandboxes[store.StatusKey(f.scope, "old")]
		require.True(t, got.Stale)
		require.Contains(t, got.Detail, "Its herdr registration was kept")
	}
	var diagnostic store.ErrorRecord
	for _, item := range status.Errors {
		if item.Code == "S0002" && item.Scope == store.StatusKey(f.scope, "old") {
			diagnostic = item
		}
	}
	require.NotEqualValues(t, "", diagnostic.Ref)
	require.Contains(t, diagnostic.Detail, "unavailable")
	require.Contains(t, diagnostic.Detail, "registration was kept")
	require.False(t, store.SeverityForCode(diagnostic.Code) != store.SeverityWarning)
	f.request = func(r *http.Request) (*http.Response, error) {
		return lifecycleResponse(200, `[{"uuid":"old","name":"demo","status":"stopped"},{"uuid":"new","name":"new","status":"stopped"}]`), nil
	}
	f.scheduler().recoverMissedDeletes()
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	{
		got := status.Sandboxes[store.StatusKey(f.scope, "old")]
		require.False(t, got.Stale)
		require.NotContains(t, got.Detail, "deleted")
	}
	require.Len(t, status.Warnings, 0, "recovered warning remains: %+v", status.Warnings)
	require.Len(t, status.Errors, 0, "recovered diagnostic remains: %+v", status.Errors)
}

func TestCatchupClearsDeletionWarningAfterConfirmedAbsence(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		return nil, errors.New("unavailable")
	}
	f.scheduler().recoverMissedDeletes()
	f.assertCount(1)
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		return lifecycleResponse(404, `{"code":"sandbox_not_found"}`), nil
	}
	f.scheduler().processCatchup(true)
	f.assertCount(0)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)

	warning := status.Warnings[store.StatusKey(f.scope, "old")+"/absence"]
	require.Equal(t, "", warning, "deleted sandbox retains warning: %s", warning)

	for _, item := range status.Errors {
		require.NotEqual(t, "S0002", item.Code, "deleted sandbox retains diagnostic: %+v", item)
	}
}

func TestConfirmedAbsenceClearsVerificationFailureWhenCleanupIsDeferred(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		return nil, errors.New("unavailable")
	}
	f.scheduler().recoverMissedDeletes()
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[]`), nil
		}
		return lifecycleResponse(404, `{"code":"sandbox_not_found"}`), nil
	}
	unlock, err := store.LockState()
	require.NoError(t, err)
	defer unlock()
	f.scheduler().recoverMissedDeletes()
	f.assertCount(1)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	for _, item := range status.Errors {
		require.NotEqual(t, store.CodeSandboxDeletion, item.Code, "confirmed deletion retains verification diagnostic: %+v", item)
	}

	detail := status.Sandboxes[store.StatusKey(f.scope, "old")].Detail
	require.NotContains(t, detail, "confirm", "confirmed deletion retains verification detail: %s", detail)

	require.Len(t, f.removed, 0, "deferred cleanup removed herdr machine: %v", f.removed)
}

func TestCatchupRecoversConfirmedDeletionWhenInventoryIsAmbiguous(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.scheduler().processCatchup(true)
	f.assertCount(0)
	require.Len(t, f.removed, 1, "removed = %v", f.removed)
}

// The panel's r action invokes Reconcile without starting a lifecycle watcher.
func TestManualReconcileRemovesDeletedSandbox(t *testing.T) {
	for _, inventory := range []string{`[]`, `[{"uuid":"other","name":"other","status":"stopped"}]`} {
		t.Run(inventory, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.save(ownedRegistration())
			require.NoError(t, store.RecordSandboxStatus(f.scope, sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}, "ready", "registered", false))
			original := f.request
			f.request = func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/sandbox" {
					return lifecycleResponse(200, inventory), nil
				}
				return original(r)
			}
			var output strings.Builder
			// Empty inventory can still block registration; confirmed deletion
			// must be recovered independently of that prerequisite.
			_ = f.service.registrations.Reconcile(&output)
			require.Len(t, f.removed, 1, "deleted sandbox machine remains after refresh: removed=%v output=%s", f.removed, output.String())
			f.assertCount(0)
			snapshot, err := store.Snapshot(f.scope)
			require.NoError(t, err)
			for _, status := range snapshot.Sandboxes {
				require.NotEqual(t, "old", status.SandboxID, "deleted sandbox remains in panel status: %#v", status)
			}
		})
	}
}

func TestManualReconcilePreservesUnconfirmedOrUnownedMachines(t *testing.T) {
	for _, scenario := range []string{"stopped", "unknown absence", "changed profile", "pending intent", "other scope"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLifecycleFixture(t)
			item := ownedRegistration()
			switch scenario {
			case "stopped":
				f.request = func(*http.Request) (*http.Response, error) {
					return lifecycleResponse(200, `[{"uuid":"old","name":"demo","status":"stopped"}]`), nil
				}
			case "unknown absence":
				original := f.request
				f.request = func(r *http.Request) (*http.Response, error) {
					if r.URL.Path != "/sandbox" {
						return lifecycleResponse(404, `{}`), nil
					}
					return original(r)
				}
			case "changed profile":
				f.catalog = `[{"id":"machine-1","label":"manual","target":"another.sbx","session":"default"}]`
			case "pending intent":
				item.Pending = true
			case "other scope":
				f.scope = "another-daemon"
			}
			f.save(item)
			_ = f.service.registrations.Reconcile(io.Discard)
			require.Len(t, f.removed, 0, "removed protected machine: %v", f.removed)
			f.assertCount(1)
		})
	}
}

func TestLifecycleSchedulerIgnoresOtherEventTypes(t *testing.T) {
	s := newLifecycleFixture(t).scheduler()
	require.True(t, s.enqueue(sbx.LifecycleEvent{Type: "other", Action: "deleted", SandboxID: "old"}))
	require.Len(t, s.retired, 0)
	require.Len(t, s.deletes, 0)
	require.Len(t, s.queue, 0)
}

func TestLifecycleDeletionRemovesOnlyOwnedProfileInItsScope(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	other := store.Tracked{SandboxID: "other", Label: "other desk", Target: "other.sbx", ProfileID: "other-machine"}
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: {item}, "other-scope": {other}}}))
	require.True(t, f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}))
	require.Len(t, f.removed, 1)
	f.assertCount(0)
	state, _ := store.LoadState()
	require.Len(t, state.Scopes["other-scope"], 1, "deletion changed another daemon scope")
}

func TestLifecycleDeletionRetainsCrossScopeMachineAssociation(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{f.scope: {item}, "other-scope": {item}}}))
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	require.Len(t, f.removed, 0, "ambiguous cross-scope machine was removed: %v", f.removed)
	f.assertCount(1)
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Len(t, state.Scopes["other-scope"], 1)
}

func TestLifecycleDeletionRetainsMismatchedProfile(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.catalog = `[{"id":"machine-1","label":"manual","target":"different","session":"default"}]`
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(1)
	require.Len(t, f.removed, 0, "manually changed profile removed")
}

func TestLifecycleQueueKeepsDeletionWhenFull(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	s := f.scheduler()
	for len(s.queue) < cap(s.queue) {
		s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "queued"})
	}
	require.False(t, s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "deleted", SandboxID: "old"}), "full queue accepted event")
	require.True(t, s.retired["old"])
	require.Len(t, s.deletes, 1)
	s.processDeletes()
	f.assertCount(0)
	require.Len(t, s.deletes, 0)
	require.Len(t, f.removed, 1)
}

func TestLifecycleGapRequestsCatchupWithoutCrossStreamComparison(t *testing.T) {
	s := newLifecycleFixture(t).scheduler()
	for _, seq := range []uint64{3, 5} {
		s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "old", Seq: &seq})
	}
	require.True(t, s.catchup, "sequence gap did not request catch-up")
	s.catchup = false
	s.beginStream()
	seq := uint64(100)
	s.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "old", Seq: &seq})
	require.False(t, s.catchup, "new stream compared its sequence to the previous stream")
}

func TestLifecycleDeletionKeepsPendingIntentVisible(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	item.Pending, item.ProfileID = true, ""
	f.save(item)
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(1)
	require.Len(t, f.removed, 0, "unresolved profile removed")
}

func TestLifecycleDeletionClearsStateWhenOwnedProfileIsAlreadyAbsent(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	f.catalog = `[]`
	f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"})
	f.assertCount(0)
	require.Len(t, f.removed, 0, "absent machine was removed twice")
}

func TestWorkerUsesPluginAndSessionLiveness(t *testing.T) {
	f := newLifecycleFixture(t)
	enabled := true
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		switch strings.Join(cmd.Args, " ") {
		case "plugin list --plugin lanwen.herdr-sbx --json":
			return fmt.Sprintf(`{"result":{"plugins":[{"plugin_id":"lanwen.herdr-sbx","enabled":%t}]}}`, enabled), nil
		case "status server --json":
			return `{"running":false}`, nil
		case "session list --json":
			return `{"sessions":[{"running":true,"socket_path":"new.sock"}]}`, nil
		}
		return "", errors.New("unexpected command")
	}
	require.True(t, f.service.workerAlive(), "running participating session did not keep worker alive")
	enabled = false
	require.False(t, f.service.workerAlive(), "disabled plugin kept worker alive")
}

func TestWorkerRebindsToTheOtherRunningSession(t *testing.T) {
	f := newLifecycleFixture(t)
	f.service.herdr.RebindSocket("old.sock")
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		if strings.Join(cmd.Args, " ") == "session list --json" {
			require.NotEqual(t, 0, len(cmd.Unset), "session discovery inherited worker socket")
			return `{"sessions":[{"running":false,"socket_path":"old.sock"},{"running":true,"socket_path":"new.sock"}]}`, nil
		}
		require.EqualValues(t, "status server --json", strings.Join(cmd.Args, " "))
		require.EqualValues(t, "new.sock", cmd.Env["HERDR_SOCKET_PATH"])
		return `{"running":true}`, nil
	}
	require.True(t, f.service.herdr.RebindToRunningSession(context.Background()))
	require.True(t, f.service.herdr.LocalServerRunning(context.Background()))
}

func TestWorkerAllowsABriefServerHandoffGap(t *testing.T) {
	f := newLifecycleFixture(t)
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		switch cmd.Args[0] {
		case "plugin":
			return `{"result":{"plugins":[{"plugin_id":"lanwen.herdr-sbx","enabled":true}]}}`, nil
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
		require.False(t, time.Since(started) < f.service.serverHandoff, "worker stopped before its handoff grace period")
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop after its handoff grace period")
	}
}

func TestWorkerReconnectsWhileHerdrRemainsLive(t *testing.T) {
	f := newLifecycleFixture(t)
	var attempts atomic.Int32
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		switch strings.Join(cmd.Args, " ") {
		case "plugin list --plugin lanwen.herdr-sbx --json":
			return fmt.Sprintf(`{"result":{"plugins":[{"plugin_id":"lanwen.herdr-sbx","enabled":%t}]}}`, attempts.Load() < 2), nil
		case "status server --json":
			return `{"running":true}`, nil
		case "daemon status --json":
			attempts.Add(1)
			return `{"status":"stopped","socket":"/tmp/lifecycle-test.sock"}`, nil
		}
		return f.defaultRun(ctx, cmd)
	}
	f.service.workerReconnect = time.Millisecond

	err := f.service.Worker(io.Discard)
	require.NoError(t, err)
	require.EqualValues(t, 2, attempts.Load())
}

func TestLocalServerStatusUsesSupportedJSON(t *testing.T) {
	f := newLifecycleFixture(t)
	f.run = func(_ context.Context, cmd process.Command) (string, error) {
		require.EqualValues(t, "herdr", cmd.Name)
		require.EqualValues(t, "status server --json", strings.Join(cmd.Args, " "))
		return `{"running":true}`, nil
	}
	require.True(t, f.service.herdr.LocalServerRunning(context.Background()), "server was not recognized as running")
}

func TestWatchCancelsSchedulerBeforeAWorkerRetry(t *testing.T) {
	f := newLifecycleFixture(t)
	f.save(ownedRegistration())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{})
	canceled := make(chan error, 1)
	var streamEnded atomic.Bool
	f.run = watchCancellationRunner(f, entered, canceled, &streamEnded)

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/events" {
			return &http.Response{StatusCode: 200, Body: reader}, nil
		}
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[{"id":"old","name":"demo","status":"running","workspace":"/host/project","labels":{"com.docker.sandbox.usesWorktree":"true"}}]`), nil
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
	require.Error(t, err)
	require.NoError(t, ctx.Err())
	select {
	case err := <-canceled:
		require.ErrorIs(t, err, context.Canceled, "scheduler command ended with %v", err)
	default:
		t.Fatal("watch returned before its in-flight scheduler command was canceled")
	}
}

func TestWorkerReportsStreamSyncAndReconnect(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	original := f.request
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/events" {
			return &http.Response{StatusCode: 200, Body: reader}, nil
		}
		return original(r)
	}
	synced := make(chan struct{}, 1)
	states := make(chan string, 8)
	reporter := workerReportFixture{t: t, cancel: cancel, synced: synced, states: states}
	report := reporter.report

	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		fmt.Fprintln(writer, `{"type":"sync","action":"complete"}`)
		select {
		case <-synced:
		case <-ctx.Done():
		}
		writer.Close()
	}()
	require.NoError(t, f.service.watchWithStatus(ctx, io.Discard, report))
	<-producerDone
	close(states)
	var got []string
	for state := range states {
		got = append(got, state)
	}
	require.Equal(t, "running,reconnecting", strings.Join(got, ","), "states: %v", got)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, saved.Worker)
	require.False(t, saved.Worker.LastSync.IsZero())
	require.EqualValues(t, "reconnecting", saved.Worker.State)
}

func TestPluginDefaultsChangeRequestsCatchupWithoutLifecycleEvent(t *testing.T) {
	f := newLifecycleFixture(t)
	scheduler := f.scheduler()
	scheduler.pluginDefaults, _ = store.ReadGuestPluginDefaults()
	inspections := 0
	f.run = func(ctx context.Context, cmd process.Command) (string, error) {
		if strings.Contains(strings.Join(cmd.Args, " "), "--version") {
			inspections++
		}
		return f.defaultRun(ctx, cmd)
	}
	scheduler.processPluginDefaults()
	require.Equal(t, 0, inspections, "unchanged defaults triggered reconciliation")
	require.NoError(t, store.SetGuestPluginDefault("example.plugin", true))
	scheduler.processPluginDefaults()
	require.NotEqual(t, 0, inspections, "saved choice did not trigger reconciliation")
	previous := inspections
	scheduler.processPluginDefaults()
	require.Equal(t, previous, inspections, "same choice repeatedly triggered reconciliation")
}

func TestLifecycleDeletionRetainsOwnershipWithMalformedMachineCatalog(t *testing.T) {
	for _, catalog := range []string{`[null]`, `[{}]`, `[{"label":"demo","target":"demo.sbx","session":"default"}]`} {
		t.Run(catalog, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.save(ownedRegistration())
			f.catalog = catalog
			require.False(t, f.scheduler().removeDeleted(sbx.LifecycleEvent{SandboxID: "old"}), "malformed machine catalog completed deletion")
			f.assertCount(1)
			require.Len(t, f.removed, 0, "malformed catalog removed machines: %v", f.removed)
		})
	}
}

func TestCatchupFailureShowsReasonAndCancellationStaysQuiet(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled_%t", cancelled), func(t *testing.T) {
			testCatchupFailure(t, cancelled)
		})
	}
}

func TestMissedDeletionReleasesRemovedEntryMetadataOnNameReuse(t *testing.T) {
	f := newLifecycleFixture(t)
	item := ownedRegistration()
	item.ProfileID = ""
	f.save(item)
	f.request = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sandbox" {
			return lifecycleResponse(200, `[{"uuid":"replacement","name":"demo","status":"stopped"}]`), nil
		}
		return lifecycleResponse(200, `{"uuid":"replacement","name":"demo","status":"stopped"}`), nil
	}
	f.scheduler().recoverMissedDeletes()
	f.assertCount(0)
	require.Len(t, f.removed, 0, "local cleanup removed native replacement: %v", f.removed)
}

func testCatchupFailure(t *testing.T, cancelled bool) {

	f := newLifecycleFixture(t)
	sb := sbx.Sandbox{ID: "old", Name: "demo", Status: "stopped"}
	require.NoError(t, store.RecordSandboxStatus(f.scope, sb, "ready", "last observed status", false))
	failure := errors.New("sandbox inventory fixture unavailable")
	if cancelled {
		failure = fmt.Errorf("refresh interrupted: %w", context.Canceled)
	}
	requests := 0
	f.request = func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/sandbox", r.URL.Path, "unexpected probe: %s", r.URL.Path)
		requests++
		if requests > 1 {
			return nil, failure
		}
		return lifecycleResponse(200, `[{"uuid":"other","name":"other","status":"stopped"}]`), nil
	}
	var output strings.Builder
	scheduler := f.scheduler()
	scheduler.out = &output
	scheduler.processCatchup(true)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	cached := saved.Sandboxes[store.StatusKey(f.scope, "old")]
	warning := saved.Warnings[f.scope+"/catch-up"]
	if cancelled {
		require.EqualValues(t, "", warning)
		require.False(t, cached.Stale)
		require.EqualValues(t, "last observed status", cached.Detail)
		require.NotContains(t, output.String(), "Docker Sandboxes warning:")
		return
	}
	require.True(t, cached.Stale)
	require.True(t, strings.HasPrefix(cached.Detail, "Could not refresh sandbox state: "))
	require.Contains(t, cached.Detail, failure.Error())
	require.False(t, warning != cached.Detail)
	require.Contains(t, output.String(), warning)

}

func watchCancellationRunner(f *lifecycleFixture, entered chan struct{}, canceled chan error, streamEnded *atomic.Bool) func(context.Context, process.Command) (string, error) {
	return func(commandContext context.Context, cmd process.Command) (string, error) {
		if cmd.Name == "sbx" && strings.Join(cmd.Args, " ") == "exec demo -- pwd -P" {
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
}

type workerReportFixture struct {
	t      *testing.T
	cancel context.CancelFunc
	synced chan struct{}
	states chan string
}

func (r workerReportFixture) report(state, detail string, sync bool) {

	assert.NoError(r.t, store.RecordWorkerStatus(state, detail, sync))
	if state != "" {
		r.states <- state
	}
	if sync {
		select {
		case r.synced <- struct{}{}:
		default:
		}
	}
	if state == "reconnecting" {
		r.cancel()
	}
}
