package herdr

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestGuestShellUsesDockerAgent(t *testing.T) {
	home := t.TempDir()
	socketDir, err := os.MkdirTemp("/tmp", "sbx-agent-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	require.NoError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte("case $- in *i*) ;; *) return ;; esac\nexport USER_SETTING=preserved\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(". \"$HOME/.bashrc\"\nexport USER_SETTING=preserved\n"), 0600))
	client := guestShellClient(t, home, socket)
	for range 2 {
		require.NoError(t, client.PrepareGuestShell(context.Background(), "test.sbx"))
	}
	for _, mode := range []string{"-ic", "-lic"} {
		require.Equal(t, socket, guestShellAgent(t, home, mode))
	}
	for _, name := range []string{".bashrc", ".bash_profile"} {
		content, err := os.ReadFile(filepath.Join(home, name))
		require.NoError(t, err)
		require.Contains(t, string(content), "export USER_SETTING=preserved\n")
		require.Equal(t, 1, strings.Count(string(content), "# herdr-sbx SSH agent begin"))
	}
	require.NoError(t, listener.Close())
	require.Equal(t, "/missing/herdr.sock.agent", guestShellAgent(t, home, "-ic"))
	require.NoError(t, os.WriteFile(socket, []byte("not a socket"), 0600))
	require.Equal(t, "/missing/herdr.sock.agent", guestShellAgent(t, home, "-ic"))
}

func guestShellClient(t *testing.T, home, socket string) *Client {
	t.Helper()
	return New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		require.Equal(t, "ssh", command.Name)
		script := strings.ReplaceAll(command.Args[len(command.Args)-1], "/run/ssh-agent.sock", socket)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		cmd.Env = append(os.Environ(), "HOME="+home)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}})
}

func guestShellAgent(t *testing.T, home, mode string) string {
	t.Helper()
	cmd := exec.Command("bash", mode, `printf '%s' "$SSH_AUTH_SOCK"`)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SSH_AUTH_SOCK=/missing/herdr.sock.agent"}
	output, err := cmd.Output()
	require.NoError(t, err)
	return string(output)
}

func TestGuestShellPreservesUnsafeStartupFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "unfinished block"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".bashrc")
			original := []byte("export USER_SETTING=preserved\n")
			switch kind {
			case "symlink":
				target := filepath.Join(t.TempDir(), "shared-bashrc")
				require.NoError(t, os.WriteFile(target, original, 0600))
				require.NoError(t, os.Symlink(target, path))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0700))
			case "unfinished block":
				original = append(original, []byte("# herdr-sbx SSH agent begin\n")...)
				require.NoError(t, os.WriteFile(path, original, 0600))
			}
			client := guestShellClient(t, home, "/missing/native-agent.sock")
			require.Error(t, client.PrepareGuestShell(context.Background(), "test.sbx"))
			if kind != "directory" {
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, original, after)
			}
		})
	}
}

func TestGuestShellUsesExistingLoginFile(t *testing.T) {
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte("# existing login config\n"), 0640))
			client := guestShellClient(t, home, "/missing/native-agent.sock")
			require.NoError(t, client.PrepareGuestShell(context.Background(), "test.sbx"))
			after, err := os.ReadFile(filepath.Join(home, name))
			require.NoError(t, err)
			require.Contains(t, string(after), "# existing login config\n")
			require.Contains(t, string(after), "# herdr-sbx SSH agent begin")
			info, err := os.Stat(filepath.Join(home, name))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0640), info.Mode().Perm())
			if name != ".bash_profile" {
				_, err := os.Stat(filepath.Join(home, ".bash_profile"))
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestGuestShellUpdatesOnlyOwnedBlock(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".bashrc")
	original := "export BEFORE=preserved\n# herdr-sbx SSH agent begin\nexport SSH_AUTH_SOCK=/old-agent.sock\n# herdr-sbx SSH agent end\nexport AFTER=preserved\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0600))
	client := guestShellClient(t, home, "/missing/native-agent.sock")
	require.NoError(t, client.PrepareGuestShell(context.Background(), "test.sbx"))
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "export BEFORE=preserved\nexport AFTER=preserved\n")
	require.NotContains(t, string(updated), "/old-agent.sock")
	require.NoError(t, client.PrepareGuestShell(context.Background(), "test.sbx"))
	repeated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, updated, repeated)
	profile, err := os.ReadFile(filepath.Join(home, ".profile"))
	require.NoError(t, err)
	require.Contains(t, string(profile), "# herdr-sbx SSH agent begin")
}
