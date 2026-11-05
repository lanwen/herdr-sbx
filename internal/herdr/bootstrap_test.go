package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	for _, shell := range []string{"", "/bin/sh"} {
		t.Run("default_shell="+shell, func(t *testing.T) {
			testProjectBootstrapRealServer(t, binary, shell)
		})
	}
}

func testProjectBootstrapRealServer(t *testing.T, binary, shell string) {
	t.Helper()
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
	t.Setenv("SHELL", "/bin/sh")
	if shell != "" {
		require.NoError(t, os.WriteFile(os.Getenv("HERDR_CONFIG_PATH"), []byte("[terminal]\ndefault_shell = \""+shell+"\"\n"), 0600))
	}
	marker := filepath.Join(root, "bash-started")
	for _, file := range []string{".bashrc", ".bash_profile"} {
		require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), file), []byte("printf '%s\\n' \"$BASH_VERSION\" > "+quoteShell(marker)+"\n"), 0600))
	}
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
	assertProjectPaneShell(t, ctx, binary, projectPaneShell{output: string(output), root: root, cwd: cwd, configured: shell})
	if shell == "" {
		version, err := os.ReadFile(marker)
		require.NoError(t, err, "ordinary panes must execute Bash startup files")
		require.NotEmpty(t, string(version))
	} else {
		require.NoFileExists(t, marker, "an explicit shell must override the server default")
	}
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

type projectPaneShell struct {
	output, root, cwd, configured string
}

func assertProjectPaneShell(t *testing.T, ctx context.Context, binary string, expected projectPaneShell) {
	t.Helper()
	var response struct {
		Result struct {
			Panes []struct {
				ID string `json:"pane_id"`
			} `json:"panes"`
		} `json:"result"`
	}
	require.NoError(t, decodeJSON(expected.output, &response))
	require.Len(t, response.Result.Panes, 1)
	require.NotEmpty(t, response.Result.Panes[0].ID)
	path := filepath.Join(expected.root, "pane-shell")
	command := `printf '%s\n' "$0" "$SHELL" "${BASH_VERSION-}" "$PWD" > ` + quoteShell(path)
	output, err := exec.CommandContext(ctx, binary, "--session", "default", "pane", "run", response.Result.Panes[0].ID, command).CombinedOutput()
	require.NoError(t, err, "%s", output)
	var observed string
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(path)
		observed = string(data)
		return err == nil && strings.HasSuffix(observed, expected.cwd+"\n")
	}, 5*time.Second, 20*time.Millisecond, "pane did not report its shell environment: %q", observed)
	values := strings.Split(strings.TrimSuffix(observed, "\n"), "\n")
	require.Len(t, values, 4)
	if expected.configured == "" {
		require.Equal(t, "/bin/bash", values[1], "server must replace an inherited sh default")
		require.Contains(t, values[0], "bash")
		require.NotEmpty(t, values[2])
	} else {
		require.Equal(t, expected.configured, values[1])
		require.Equal(t, "sh", filepath.Base(strings.TrimPrefix(values[0], "-")), "explicit sh must take precedence")
	}
}
