package sbx

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestGuestShellFailureIncludesOnlyAvailableOutput(t *testing.T) {
	for _, test := range []struct{ name, output, want string }{
		{"empty", "", "prepare sandbox shell: timed out"},
		{"whitespace", " \n\t ", "prepare sandbox shell: timed out"},
		{"diagnostic", " permission denied\n", "prepare sandbox shell: timed out: permission denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("timed out")
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return test.output, cause
			}})
			err := client.PrepareGuestShell(context.Background(), "test.sbx")
			require.EqualError(t, err, test.want)
			require.ErrorIs(t, err, cause)
		})
	}
}

func TestGuestShellTimeoutExplainsAttemptedWork(t *testing.T) {
	for _, output := range []string{"", " \n\t ", "connection diagnostics\n"} {
		t.Run(output, func(t *testing.T) {
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return output, process.ErrTimeout
			}})
			err := client.PrepareGuestShell(context.Background(), "test.sbx")
			want := "sandbox preparation timed out when trying to update Bash startup files for Docker's forwarded SSH agent"
			if detail := strings.TrimSpace(output); detail != "" {
				want += ": " + detail
			}
			require.EqualError(t, err, want)
			require.ErrorIs(t, err, process.ErrTimeout)
		})
	}
}

func TestGuestShellPreparationBypassesStartupHooks(t *testing.T) {
	home := t.TempDir()
	hook := filepath.Join(home, "startup-hook")
	require.NoError(t, os.WriteFile(hook, []byte("printf 'startup hook executed\\n' >&2\nexit 73\n"), 0600))
	client := New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		var cmd *exec.Cmd
		if command.Name == "ssh" {
			// Model sandboxd's outer Bash, which reads BASH_ENV before the command.
			cmd = exec.CommandContext(ctx, "/bin/bash", "-c", command.Args[len(command.Args)-1])
		} else {
			separator := slices.Index(command.Args, "--")
			require.Positive(t, separator)
			argv := command.Args[separator+1:]
			cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
		}
		cmd.Env = append(os.Environ(), "HOME="+home, "BASH_ENV="+hook)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}})
	require.NoError(t, client.PrepareGuestShell(context.Background(), "test"))
	content, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	require.NoError(t, err)
	require.Contains(t, string(content), "# herdr-sbx SSH agent begin")
	require.Contains(t, string(content), "export SSH_AUTH_SOCK=/run/ssh-agent.sock")
}

func TestGuestShellPreparationUsesScopedDirectExec(t *testing.T) {
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "herdr-sbx-test-shell")
	client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		require.Equal(t, "sbx", command.Name)
		require.Equal(t, []string{"--app-name", "herdr-sbx-test-shell", "exec", "test", "--", "env", "-u", "BASH_ENV", "/bin/bash", "--noprofile", "--norc", "-c", guestShellSetup}, command.Args)
		require.Equal(t, 15*time.Second, command.Timeout)
		return "", nil
	}})
	require.NoError(t, client.PrepareGuestShell(context.Background(), "test"))
}

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
		require.Equal(t, "sbx", command.Name)
		separator := slices.Index(command.Args, "--")
		require.Positive(t, separator)
		argv := append([]string(nil), command.Args[separator+1:]...)
		argv[len(argv)-1] = strings.ReplaceAll(argv[len(argv)-1], "/run/ssh-agent.sock", socket)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
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
