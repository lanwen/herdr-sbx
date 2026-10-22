package sbx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

type daemonTestTransport func(*http.Request) (*http.Response, error)

func (f daemonTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSandboxAbsenceRequiresA404(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		absent bool
		err    bool
	}{
		{"named absence", 404, `{"message":"sandbox \"demo\" not found"}`, true, false},
		{"coded absence", 404, `{"code":"sandbox_not_found"}`, true, false},
		{"present", 200, `{"uuid":"old","name":"demo","status":"running"}`, false, false},
		{"replacement", 200, `{"uuid":"new","name":"demo","status":"running"}`, false, true},
		{"generic 404", 404, `{"message":"Not Found"}`, false, true},
		{"unavailable", 503, ``, false, true},
		{"missing identity", 200, `{"name":"demo"}`, false, true},
		{"wrong sandbox", 200, `{"uuid":"new","name":"different"}`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewWithTransport(process.Runner{}, daemonTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/sandbox/demo" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			}))
			absent, err := client.SandboxAbsent(context.Background(), "unused", "demo", "old")
			if errors.Is(err, ErrSandboxIdentityChanged) != (test.name == "replacement") {
				t.Fatalf("incorrect replacement proof: %v", err)
			}
			if absent != test.absent || (err != nil) != test.err {
				t.Fatalf("absence = %v, %v", absent, err)
			}
		})
	}
}

func TestLifecycleReaderAcceptsInitialReplayAndUnknownFields(t *testing.T) {
	stream := `{"type":"sandbox.lifecycle","action":"started","sandbox_id":"s1","sandbox_name":"demo","extra":true}` + "\n" +
		`{"type":"sandbox.lifecycle","action":"sync","unknown":"ok"}` + "\n"
	client := NewWithTransport(process.Runner{}, daemonTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.RequestURI() != "/events?type=sandbox.lifecycle" {
			t.Fatalf("unexpected stream request: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}, nil
	}))
	var got []LifecycleEvent
	err := client.ReadLifecycle(context.Background(), "unused", func(event LifecycleEvent) bool {
		got = append(got, event)
		return true
	})
	if err != nil || len(got) != 2 || got[0].Action != "started" || got[0].SandboxID != "s1" || got[1].Action != "sync" {
		t.Fatalf("events = %#v, err = %v", got, err)
	}
}

func TestDaemonClientDoesNotKeepIdleConnections(t *testing.T) {
	transport, ok := New(process.Runner{}).httpClient("unused").Transport.(*http.Transport)
	if !ok || !transport.DisableKeepAlives {
		t.Fatalf("transport = %#v", transport)
	}
}

func TestSandboxAppScopeIsExplicit(t *testing.T) {
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "isolated")
	var calls []string
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		calls = append(calls, cmd.Name+" "+strings.Join(cmd.Args, " "))
		return `{"status":"running","socket":"/tmp/fake.sock","checks":[{"name":"Authentication","status":"pass"}]}`, nil
	}})
	ctx := context.Background()
	_, _ = client.Version(ctx)
	client.JSONCommandsAvailable(ctx)
	_, _ = client.DaemonStatus(ctx)
	client.DiagnoseAuthentication(ctx)
	_, _ = client.WorkspaceDirectory(ctx, "demo")
	want := []string{
		"sbx version", "sbx daemon status --help", "sbx ls --help",
		"sbx --app-name isolated daemon status --json",
		"sbx --app-name isolated diagnose --json",
		"sbx --app-name isolated exec demo -- pwd -P",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %q, want %q", calls, want)
	}
}

func TestInventoryEnvelopeAndDaemonStatusNoise(t *testing.T) {
	client := NewWithTransport(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return "warning: settings unavailable\n{\"status\":\"stopped\"}\n", nil
	}}, daemonTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"result":{"sandboxes":[{"id":"s1","name":"demo","state":"running"}]}}`))}, nil
	}))
	sandboxes, err := client.Inventory(context.Background(), "unused")
	if err != nil || len(sandboxes) != 1 || sandboxes[0].ID != "s1" || sandboxes[0].Status != "running" {
		t.Fatalf("inventory=%#v err=%v", sandboxes, err)
	}
	daemon, err := client.DaemonStatus(context.Background())
	if err != nil || daemon.Status != "stopped" {
		t.Fatalf("daemon=%#v err=%v", daemon, err)
	}
}

func TestInventoryWorkspaceMetadata(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata string
		probe    bool
	}{
		{"direct", ``, false},
		{"clone", `,"source_repo_dir":"/host/source","labels":{"com.docker.sandbox.cloneMode":"true"}`, false},
		{"matching directory", `,"labels":{"com.docker.sandbox.workingDirectory":"/project","com.docker.sandbox.usesWorktree":"false"}`, false},
		{"legacy worktree", `,"labels":{"com.docker.sandbox.usesWorktree":"true"}`, true},
		{"worktree override", `,"labels":{"com.docker.sandbox.worktreePath":"/different"}`, true},
		{"different directory", `,"labels":{"com.docker.sandbox.workingDirectory":"/different"}`, true},
		{"unknown labels", `,"labels":[]`, true},
		{"invalid directory label", `,"labels":{"com.docker.sandbox.workingDirectory":42}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewWithTransport(process.Runner{}, daemonTestTransport(func(*http.Request) (*http.Response, error) {
				body := `[{"uuid":"s1","name":"demo","status":"running","workspace":"/project"` + test.metadata + `}]`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			items, err := client.Inventory(context.Background(), "unused")
			if err != nil || len(items) != 1 {
				t.Fatalf("inventory=%#v err=%v", items, err)
			}
			if items[0].Workspace != "/project" || items[0].WorkspaceNeedsProbe != test.probe {
				t.Fatalf("sandbox=%#v", items[0])
			}
			encoded, err := json.Marshal(items[0])
			if err != nil || strings.Contains(string(encoded), "NeedsProbe") || strings.Contains(string(encoded), "labels") {
				t.Fatalf("internal metadata exposed: %s (%v)", encoded, err)
			}
		})
	}
}

func TestInventoryRejectsNonStringWorkspace(t *testing.T) {
	for _, workspace := range []string{`null`, `42`, `true`, `{}`, `[]`} {
		t.Run(workspace, func(t *testing.T) {
			body := `{"uuid":"s1","name":"demo","workspace":` + workspace + `}`
			client := NewWithTransport(process.Runner{}, daemonTestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("[" + body + "]"))}, nil
			}))
			if _, err := client.Inventory(context.Background(), "unused"); err == nil || !strings.Contains(err.Error(), "workspace is not a string") {
				t.Fatalf("inventory error=%v", err)
			}
			if _, err := parseSandbox(body); err == nil || !strings.Contains(err.Error(), "workspace is not a string") {
				t.Fatalf("sandbox error=%v", err)
			}
		})
	}
}
