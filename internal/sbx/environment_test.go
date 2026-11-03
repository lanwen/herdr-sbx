package sbx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestGuestManagementHostEnvironment(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("POSIX sbx fixture")
	}
	dir := t.TempDir()
	report := filepath.Join(dir, "environment")
	fixture := "#!/bin/sh\nprintf '%s\\n' \"${SSH_AUTH_SOCK+x}\" \"${SSH_AUTH_SOCK-}\" \"$SBX_ENV_MARKER\" > \"$SBX_ENV_REPORT\"\nprintf '/workspace\\n'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sbx"), []byte(fixture), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SSH_AUTH_SOCK", "/missing/herdr.sock.agent")
	t.Setenv("SBX_ENV_MARKER", "preserved")
	t.Setenv("SBX_ENV_REPORT", report)
	client := New(process.Runner{})
	for _, test := range []struct {
		name string
		run  func() error
		want string
	}{
		{"prepare shell", func() error { return client.PrepareGuestShell(context.Background(), "test") }, "\n\npreserved\n"},
		{"workspace", func() error {
			_, err := client.WorkspaceDirectory(context.Background(), "test")
			return err
		}, "\n\npreserved\n"},
		{"version", func() error { _, err := client.Version(context.Background()); return err }, "x\n/missing/herdr.sock.agent\npreserved\n"},
		{"setup ssh", func() error { return client.SetupSSHInteractive(context.Background()) }, "x\n/missing/herdr.sock.agent\npreserved\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.run())
			got, err := os.ReadFile(report)
			require.NoError(t, err)
			require.Equal(t, test.want, string(got))
			require.Equal(t, "/missing/herdr.sock.agent", os.Getenv("SSH_AUTH_SOCK"))
		})
	}
}

func TestGuestManagementEnvironmentPolicy(t *testing.T) {
	client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			require.Equal(t, []string{"SSH_AUTH_SOCK"}, command.Unset)
		} else {
			// Windows must retain its agent selection instead of using the native fallback.
			require.Empty(t, command.Unset)
		}
		require.Empty(t, command.Env)
		return "/workspace", nil
	}})
	require.NoError(t, client.PrepareGuestShell(context.Background(), "test"))
	directory, err := client.WorkspaceDirectory(context.Background(), "test")
	require.NoError(t, err)
	require.Equal(t, "/workspace", directory)
}
