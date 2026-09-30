package panel

import (
	"bufio"
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
	"github.com/lanwen/herdr-sbx/internal/store"
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
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	runner := process.Runner{Interactive: interactive, Execute: func(_ context.Context, cmd process.Command) (string, error) {
		args := cmd.Args
		if len(args) > 1 && args[0] == "--app-name" && args[1] == "isolated" {
			args = args[2:]
		}
		switch cmd.Name + " " + strings.Join(args, " ") {
		case "herdr --version":
			return "herdr 0.9.1", nil
		case "sbx --version":
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
		if r.Method != "GET" || r.URL.Path != "/sandbox" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"id":"second","name":"zeta","status":"running"},{"id":"stopped","name":"aaa","status":"stopped"},{"id":"first","name":"alpha","status":"running"}]`))}, nil
	}))
	herdrClient := herdr.New(runner)
	checks := precheck.New(sandboxes, herdrClient, runner)
	registrations := registration.New(sandboxes, herdrClient, checks, runner)
	return New(registrations, lifecycle.New(registrations, sandboxes, herdrClient), sandboxes)
}

func TestRepairRunsOnlyTheChosenInteractiveCommandThenRefreshes(t *testing.T) {
	for action, want := range map[string]string{"daemon": "daemon start", "login": "login", "ssh": "setup ssh"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			ui := testPanel(t, func(_ context.Context, name string, args ...string) error {
				calls++
				if name != "sbx" || strings.Join(args, " ") != "--app-name isolated "+want {
					t.Fatalf("interactive command = %s %v", name, args)
				}
				return nil
			})
			var out bytes.Buffer
			if err := ui.Repair(action, &out); err != nil || calls != 1 || !strings.Contains(out.String(), "Refreshing status.") || !strings.Contains(out.String(), "Sandbox daemon") {
				t.Fatalf("repair calls=%d err=%v output=%s", calls, err, out.String())
			}
		})
	}
}

func TestRepairCancellationRefreshesWithoutRetry(t *testing.T) {
	if os.Getenv("HERDR_SBX_REPAIR_CANCEL_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRepairCancellationRefreshesWithoutRetry$")
		child.Env = append(os.Environ(), "HERDR_SBX_REPAIR_CANCEL_CHILD=1")
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("repair cancellation subprocess: %v\n%s", err, output)
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
	if err := ui.Repair("login", &out); err != nil || calls != 1 || !strings.Contains(out.String(), "canceled.") || !strings.Contains(out.String(), "Refreshing status.") || !strings.Contains(out.String(), "Sandbox daemon") {
		t.Fatalf("cancellation calls=%d err=%v output=%s", calls, err, out.String())
	}
}

func TestSandboxSelectionUsesListedIdentity(t *testing.T) {
	ui := testPanel(t, func(context.Context, string, ...string) error { t.Fatal("selection attempted repair"); return nil })
	var out bytes.Buffer
	_, selected, err := ui.chooseSandbox(bufio.NewReader(strings.NewReader("invalid\n0\n3\n2\n")), &out, true)
	if err != nil || selected.ID != "second" || !strings.Contains(out.String(), "1  alpha (running)") || !strings.Contains(out.String(), "2  zeta (running)") || strings.Contains(out.String(), "aaa") {
		t.Fatalf("selection=%#v err=%v output=%s", selected, err, out.String())
	}
	_, selected, err = ui.chooseSandbox(bufio.NewReader(strings.NewReader("1\n")), io.Discard, false)
	if err != nil || selected.ID != "stopped" {
		t.Fatalf("retry selection=%#v err=%v", selected, err)
	}
	_, _, err = ui.chooseSandbox(bufio.NewReader(strings.NewReader("\n")), io.Discard, true)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestSandboxStatusSeparatesObservationFromConnectionAndStaleness(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sandbox := sbx.Sandbox{ID: "old", Name: "demo", Status: "running"}
	if err := store.RecordSandboxStatus("scope", sandbox, "preparing", "installing", false); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkScopeStale("scope", "sandbox discovery is unavailable"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printSandboxStatuses(&out, "scope")
	for _, want := range []string{"observed: running", "connection: preparing (stale)", "sandbox discovery is unavailable"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
