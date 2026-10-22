package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

// Opt in with SBX_TEST_HERDR_BINARY. Only the transport is substituted:
// the guest shell command runs against a real, isolated local Herdr server.
func TestProjectBootstrapRealServer(t *testing.T) {
	binary := os.Getenv("SBX_TEST_HERDR_BINARY")
	if binary == "" {
		t.Skip("set SBX_TEST_HERDR_BINARY for the isolated real-server gate")
	}
	root, err := os.MkdirTemp("/tmp", "sbx-cwd-")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	for key, dir := range map[string]string{"HOME": "h", "XDG_CONFIG_HOME": "c", "XDG_STATE_HOME": "s", "XDG_RUNTIME_DIR": "r"} {
		path := filepath.Join(root, dir)
		require.NoError(t, os.MkdirAll(path, 0700))
		t.Setenv(key, path)
	}
	for _, key := range []string{"HERDR_SOCKET_PATH", "HERDR_CLIENT_SOCKET_PATH", "HERDR_SESSION", "HERDR_STARTUP_CWD"} {
		t.Setenv(key, "")
	}
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(root, "c", "config.toml"))
	bin := filepath.Join(root, "bin")
	require.NoError(t, os.Mkdir(bin, 0700))
	require.NoError(t, os.Symlink(binary, filepath.Join(bin, "herdr")))
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = exec.CommandContext(stopCtx, binary, "--session", "default", "server", "stop").Run()
	}()
	client := New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		require.Equal(t, "ssh", command.Name, "unexpected command: %#v", command)
		out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1]).CombinedOutput()
		return string(out), err
	}})
	running, err := client.GuestServerRunning(ctx, "test.sbx")
	require.NoError(t, err)
	require.False(t, running)
	cwd := filepath.Join(root, "project ' spaces")
	require.NoError(t, os.Mkdir(cwd, 0700))
	// Match pwd -P in a sandbox (macOS resolves /tmp to /private/tmp).
	cwd, err = filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, client.StartProjectServer(ctx, "test.sbx", cwd))
	}
	running, err = client.GuestServerRunning(ctx, "test.sbx")
	require.NoError(t, err)
	require.True(t, running)
	output, err := exec.CommandContext(ctx, binary, "--session", "default", "pane", "list").CombinedOutput()
	require.NoError(t, err, "panes: %v %s", err, output)
	panes, err := parsePanes(string(output))
	require.NoError(t, err)
	require.Len(t, panes, 1)
	require.False(t, panes[0].CWD != cwd)
	other := filepath.Join(root, "another-project")
	require.NoError(t, os.Mkdir(other, 0700))
	{
		err := client.StartProjectServer(ctx, "test.sbx", other)
		require.Error(t, err, "existing session was not preserved")
	}
	output, err = exec.CommandContext(ctx, binary, "--session", "default", "pane", "list").CombinedOutput()
	require.NoError(t, err)
	panes, err = parsePanes(string(output))
	require.NoError(t, err)
	require.Len(t, panes, 1)
	require.False(t, panes[0].CWD != cwd)
}
