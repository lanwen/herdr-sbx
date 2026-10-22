package panel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panelTransport func(*http.Request) (*http.Response, error)

func (f panelTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testPanel(t *testing.T, interactive func(context.Context, string, ...string) error) *Service {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "herdr")
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "isolated")
	bin := t.TempDir()
	for _, name := range []string{"herdr", "sbx", "ssh"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0700))
	}
	t.Setenv("PATH", bin)
	runner := process.Runner{Interactive: interactive, Execute: func(_ context.Context, cmd process.Command) (string, error) {
		args := cmd.Args
		if len(args) > 1 && args[0] == "--app-name" && args[1] == "isolated" {
			args = args[2:]
		}
		switch cmd.Name + " " + strings.Join(args, " ") {
		case "herdr plugin list --json":
			return `[{"plugin_id":"first.plugin","enabled":true},{"plugin_id":"second.plugin","enabled":true}]`, nil
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/panel-test.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		}
		t.Errorf("unexpected passive command: %#v", cmd)
		return "", errors.New("unexpected command")
	}}
	sandboxes := sbx.NewWithTransport(runner, panelTransport(func(r *http.Request) (*http.Response, error) {
		assert.False(t, r.Method != "GET" || r.URL.Path != "/sandbox", "unexpected request: %s %s", r.Method, r.URL)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"id":"second","name":"zeta","status":"running"},{"id":"stopped","name":"aaa","status":"stopped"},{"id":"first","name":"alpha","status":"running"}]`))}, nil
	}))
	herdrClient := herdr.New(runner)
	checks := precheck.New(sandboxes, herdrClient, runner)
	registrations := registration.New(sandboxes, herdrClient, checks, runner)
	return New(registrations, lifecycle.New(registrations, sandboxes, herdrClient), sandboxes)
}

func TestSSHSetupRefreshesStatus(t *testing.T) {
	calls := 0
	ui := testPanel(t, func(_ context.Context, name string, args ...string) error {
		calls++
		require.EqualValues(t, "sbx", name)
		require.EqualValues(t, "--app-name isolated setup ssh", strings.Join(args, " "))
		return nil
	})
	var out bytes.Buffer

	err := ui.Repair("ssh", &out)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls)
	require.Contains(t, out.String(), "Refreshing status.")
	require.Contains(t, out.String(), "Sandbox daemon")
}

func TestRepairCancellationRefreshesWithoutRetry(t *testing.T) {
	if os.Getenv("HERDR_SBX_REPAIR_CANCEL_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRepairCancellationRefreshesWithoutRetry$")
		child.Env = append(os.Environ(), "HERDR_SBX_REPAIR_CANCEL_CHILD=1")
		{
			output, err := child.CombinedOutput()
			require.NoError(t, err, "repair cancellation subprocess: %v\n%s", err, output)
		}
		return
	}
	calls := 0
	ui := testPanel(t, func(ctx context.Context, _ string, _ ...string) error {
		calls++
		self, err := os.FindProcess(os.Getpid())
		if err != nil {
			return err
		}
		if err := self.Signal(os.Interrupt); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	var out bytes.Buffer
	{
		err := ui.Repair("ssh", &out)
		require.NoError(t, err)
		require.EqualValues(t, 1, calls)
		require.Contains(t, out.String(), "canceled.")
		require.Contains(t, out.String(), "Refreshing status.")
		require.Contains(t, out.String(), "Sandbox daemon")
	}
}
