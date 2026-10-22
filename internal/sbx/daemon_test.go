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
	"github.com/stretchr/testify/require"
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
				require.False(t, r.Method != http.MethodGet)
				require.EqualValues(t, "/sandbox/demo", r.URL.Path)
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			}))
			absent, err := client.SandboxAbsent(context.Background(), "unused", "demo", "old")
			require.Equal(t, (test.name == "replacement"), errors.Is(err, ErrSandboxIdentityChanged), "incorrect replacement proof: %v", err)
			require.False(t, absent != test.absent)
			require.False(t, (err != nil) != test.err)
		})
	}
}

func TestLifecycleReaderAcceptsInitialReplayAndUnknownFields(t *testing.T) {
	stream := `{"type":"sandbox.lifecycle","action":"started","sandbox_id":"s1","sandbox_name":"demo","extra":true}` + "\n" +
		`{"type":"sandbox.lifecycle","action":"sync","unknown":"ok"}` + "\n"
	client := NewWithTransport(process.Runner{}, daemonTestTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/events?type=sandbox.lifecycle", r.URL.RequestURI(), "unexpected stream request: %s", r.URL)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}, nil
	}))
	var got []LifecycleEvent
	err := client.ReadLifecycle(context.Background(), "unused", func(event LifecycleEvent) bool {
		got = append(got, event)
		return true
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.EqualValues(t, "started", got[0].Action)
	require.EqualValues(t, "s1", got[0].SandboxID)
	require.EqualValues(t, "sync", got[1].Action)
}

func TestDaemonClientDoesNotKeepIdleConnections(t *testing.T) {
	transport, ok := New(process.Runner{}).httpClient("unused").Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.DisableKeepAlives)
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
	require.Equal(t, strings.Join(want, "\n"), strings.Join(calls, "\n"), "commands = %q, want %q", calls, want)
}

func TestInventoryEnvelopeAndDaemonStatusNoise(t *testing.T) {
	client := NewWithTransport(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return "warning: settings unavailable\n{\"status\":\"stopped\"}\n", nil
	}}, daemonTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"result":{"sandboxes":[{"id":"s1","name":"demo","state":"running"}]}}`))}, nil
	}))
	sandboxes, err := client.Inventory(context.Background(), "unused")
	require.NoError(t, err)
	require.Len(t, sandboxes, 1)
	require.EqualValues(t, "s1", sandboxes[0].ID)
	require.EqualValues(t, "running", sandboxes[0].Status)
	daemon, err := client.DaemonStatus(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, "stopped", daemon.Status)
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
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.EqualValues(t, "/project", items[0].Workspace)
			require.False(t, items[0].WorkspaceNeedsProbe != test.probe)
			encoded, err := json.Marshal(items[0])
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "NeedsProbe")
			require.NotContains(t, string(encoded), "labels")
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
			{
				_, err := client.Inventory(context.Background(), "unused")
				require.Error(t, err)
				require.Contains(t, err.Error(), "workspace is not a string")
			}
			{
				_, err := parseSandbox(body)
				require.Error(t, err)
				require.Contains(t, err.Error(), "workspace is not a string")
			}
		})
	}
}
