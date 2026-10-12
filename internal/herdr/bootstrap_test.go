package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// Opt in with SBX_TEST_HERDR_BINARY. Only the transport is substituted:
// the guest shell command runs against a real, isolated local Herdr server.
func TestProjectBootstrapRealServer(t *testing.T) {
	binary := os.Getenv("SBX_TEST_HERDR_BINARY")
	if binary == "" {
		t.Skip("set SBX_TEST_HERDR_BINARY for the isolated real-server gate")
	}
	root, err := os.MkdirTemp("/tmp", "sbx-cwd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	for key, dir := range map[string]string{"HOME": "h", "XDG_CONFIG_HOME": "c", "XDG_STATE_HOME": "s", "XDG_RUNTIME_DIR": "r"} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, path)
	}
	for _, key := range []string{"HERDR_SOCKET_PATH", "HERDR_CLIENT_SOCKET_PATH", "HERDR_SESSION", "HERDR_STARTUP_CWD"} {
		t.Setenv(key, "")
	}
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(root, "c", "config.toml"))
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, filepath.Join(bin, "herdr")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = exec.CommandContext(stopCtx, binary, "--session", "default", "server", "stop").Run()
	}()
	client := New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		if command.Name != "ssh" {
			t.Fatalf("unexpected command: %#v", command)
		}
		out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1]).CombinedOutput()
		return string(out), err
	}})
	running, err := client.GuestServerRunning(ctx, "test.sbx")
	if err != nil || running {
		t.Fatalf("initial running=%v err=%v", running, err)
	}
	cwd := filepath.Join(root, "project ' spaces")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	// Match pwd -P in a sandbox (macOS resolves /tmp to /private/tmp).
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := client.StartProjectServer(ctx, "test.sbx", cwd); err != nil {
			t.Fatal(err)
		}
	}
	running, err = client.GuestServerRunning(ctx, "test.sbx")
	if err != nil || !running {
		t.Fatalf("started running=%v err=%v", running, err)
	}
	output, err := exec.CommandContext(ctx, binary, "--session", "default", "pane", "list").CombinedOutput()
	if err != nil {
		t.Fatalf("panes: %v %s", err, output)
	}
	panes, err := parsePanes(string(output))
	if err != nil || len(panes) != 1 || panes[0].CWD != cwd {
		t.Fatalf("panes=%#v err=%v", panes, err)
	}
	other := filepath.Join(root, "another-project")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := client.StartProjectServer(ctx, "test.sbx", other); err == nil {
		t.Fatal("existing session was not preserved")
	}
	output, err = exec.CommandContext(ctx, binary, "--session", "default", "pane", "list").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	panes, err = parsePanes(string(output))
	if err != nil || len(panes) != 1 || panes[0].CWD != cwd {
		t.Fatalf("existing workspace changed: %#v %v", panes, err)
	}
}
