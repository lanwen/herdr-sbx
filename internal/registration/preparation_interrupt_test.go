package registration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

// A signal during a real download must close the response and remove the
// temporary file before the manual action returns.
func TestManualPreparationInterrupt(t *testing.T) {
	if helper := os.Getenv("HERDR_TEST_INTERRUPT_HELPER"); helper != "" {
		runPreparationInterruptHelper(t, helper)
		return
	}
	for _, action := range []string{"reconcile", "retry"} {
		t.Run(action, func(t *testing.T) {
			testManualPreparationInterruptScenario(t, action)
		})
	}
}

func runPreparationInterruptHelper(t *testing.T, action string) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := command.Name + " " + strings.Join(command.Args, " ")
		switch joined {
		case "herdr --version":
			return "herdr 0.9.3\n", nil
		case "sbx version":
			return "sbx 0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/unused-isolated-daemon.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return missingRemoteHerdr + "\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx uname -sm":
			return "Linux x86_64", nil
		case "herdr machine list --json":
			return "[]", nil
		}
		require.NotContains(t, joined, "machine add", "canceled preparation registered a machine")
		return "", fmt.Errorf("unexpected command %s", joined)
	}
	fixture.rebuildClients()
	fixture.service.releaseTransport = releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == releaseManifestURL {
			data, _ := json.Marshal(herdrRelease{Version: "0.9.3", Assets: map[string]string{"linux-x86_64": "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64"}, SHA256: map[string]string{"linux-x86_64": strings.Repeat("0", 64)}})
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &interruptDownload{ctx: request.Context()}, Request: request}, nil
	})
	if action == "retry" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		_ = fixture.service.RetrySelected(ctx, io.Discard, SandboxSelection{Scope: "scope", Socket: "/unused-isolated-daemon.sock", ID: "sandbox-1", Name: "demo"})
		return
	}
	_ = fixture.service.ReconcileSelected(io.Discard, nil)
}

type interruptDownload struct {
	ctx     context.Context
	started bool
}

func (reader *interruptDownload) Read(buffer []byte) (int, error) {
	if !reader.started {
		reader.started = true
		return copy(buffer, "partial download"), nil
	}
	fmt.Println("download ready")
	<-reader.ctx.Done()
	return 0, reader.ctx.Err()
}

func (*interruptDownload) Close() error { return nil }

func testManualPreparationInterruptScenario(t *testing.T, action string) {
	t.Helper()

	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManualPreparationInterrupt$")
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "HERDR") && !strings.HasPrefix(item, "XDG") && !strings.HasPrefix(item, "HOME=") && !strings.HasPrefix(item, "TMPDIR=") {
			env = append(env, item)
		}
	}
	command.Env = append(append([]string(nil), env...), "HERDR_TEST_INTERRUPT_HELPER="+action, "HOME="+root, "TMPDIR="+root, "HERDR_PLUGIN_STATE_DIR="+filepath.Join(root, "state"))
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	var stderr strings.Builder
	command.Stderr = &stderr
	require.NoError(t, command.Start())
	scanner := bufio.NewScanner(stdout)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "download ready" {
			ready = true
			break
		}
	}
	if !ready {
		_ = command.Wait()
		t.Fatalf("helper failed before download: %s", stderr.String())
	}
	require.NoError(t, command.Process.Signal(os.Interrupt))
	_, _ = io.Copy(io.Discard, stdout)
	{
		err := command.Wait()
		require.NoError(t, err, "signal did not unwind cleanly: %v %s", err, stderr.String())
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, "herdr-sbx-download-*"))
	require.Len(t, leftovers, 0, "download leaked: %v", leftovers)
}
