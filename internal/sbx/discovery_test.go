package sbx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func discoveryClient(t *testing.T, runner process.Runner) *Client {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	root := t.TempDir()
	client := New(runner)
	client.discovery = &discoveryOptions{platform: "darwin", paths: []string{filepath.Join(root, "arm-sbx"), filepath.Join(root, "intel-sbx")}}
	return client
}

func installCandidate(t *testing.T, client *Client, index int) string {
	t.Helper()
	path := client.discovery.paths[index]
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 99\n"), 0700))
	return path
}

func inventoryTransport(body string, status int, err error) daemonTestTransport {
	return func(r *http.Request) (*http.Response, error) {
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

func TestDaemonDiscoveryPrefersPATHAndPreservesErrors(t *testing.T) {
	for _, test := range []struct {
		name, output string
		err          error
	}{
		{"running", `{"status":"running","socket":"reported"}`, nil},
		{"stopped", `{"status":"stopped"}`, nil},
		{"exit", "failure", &exec.ExitError{}},
		{"timeout", "", process.ErrTimeout},
		{"permission", "", os.ErrPermission},
		{"malformed", "invalid", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
				calls++
				require.Equal(t, "sbx", cmd.Name)
				return test.output, test.err
			}})
			client.transport = daemonTestTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected socket probe"); return nil, nil })
			daemon, err := client.DaemonStatus(context.Background())
			require.Equal(t, 1, calls)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
			} else if test.name == "malformed" {
				require.ErrorIs(t, err, ErrInvalidDaemonStatus)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.name, daemon.Status)
			}
		})
	}
}

func TestDaemonDiscoveryValidatesSocketInventory(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		err        error
		valid      bool
	}{
		{"inventory", `{"sandboxes":[{"id":"one","name":"demo"}]}`, 200, nil, true},
		{"empty", `{"result":{"sandboxes":[]}}`, 200, nil, true},
		{"array", `[]`, 200, nil, true},
		{"missing envelope", `{}`, 200, nil, false},
		{"malformed", `{"sandboxes":`, 200, nil, false},
		{"null", `{"sandboxes":null}`, 200, nil, false},
		{"bad record", `{"sandboxes":[42]}`, 200, nil, false},
		{"http error", `{}`, 503, nil, false},
		{"permission", "", 0, os.ErrPermission, false},
		{"deadline", "", 0, context.DeadlineExceeded, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
				calls++
				require.Equal(t, "sbx", cmd.Name)
				return "", fmt.Errorf("lookup: %w", exec.ErrNotFound)
			}})
			installCandidate(t, client, 0)
			client.transport = inventoryTransport(test.body, test.status, test.err)
			daemon, err := client.DaemonStatus(context.Background())
			require.Equal(t, 1, calls)
			if !test.valid {
				require.Error(t, err)
				if test.err != nil {
					require.ErrorIs(t, err, test.err)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "running", daemon.Status)
			require.Equal(t, filepath.Join(os.Getenv("HOME"), "Library/Application Support/com.docker.sandboxes/sandboxes/sandboxd/sandboxd.sock"), daemon.Socket)
		})
	}
}

func TestDaemonDiscoveryUsesHomebrewAfterAbsentSocket(t *testing.T) {
	for _, socketErr := range []error{os.ErrNotExist, syscall.ECONNREFUSED} {
		t.Run(socketErr.Error(), func(t *testing.T) {
			var calls []string
			client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
				calls = append(calls, cmd.Name)
				if cmd.Name == "sbx" {
					return "", exec.ErrNotFound
				}
				return `{"status":"running","socket":"brew.sock"}`, nil
			}})
			client.transport = inventoryTransport("", 0, socketErr)
			arm := installCandidate(t, client, 0)
			intel := installCandidate(t, client, 1)
			daemon, err := client.DaemonStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, "brew.sock", daemon.Socket)
			require.Equal(t, []string{"sbx", arm}, calls)
			require.NoError(t, os.Chmod(arm, 0600))
			calls = nil
			_, err = client.DaemonStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, []string{"sbx", intel}, calls)
			require.NoError(t, os.Remove(intel))
			calls = nil
			_, err = client.DaemonStatus(context.Background())
			require.ErrorIs(t, err, exec.ErrNotFound)
			require.Contains(t, err.Error(), arm)
			require.Contains(t, err.Error(), intel)
		})
	}
}

func TestDaemonDiscoverySkipsDefaultSocketForCustomApp(t *testing.T) {
	var calls []process.Command
	client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		calls = append(calls, cmd)
		if cmd.Name == "sbx" {
			return "", exec.ErrNotFound
		}
		return "broken status", nil
	}})
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "custom")
	installCandidate(t, client, 0)
	client.transport = daemonTestTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("custom app probed default socket")
		return nil, nil
	})
	_, err := client.DaemonStatus(context.Background())
	require.ErrorIs(t, err, ErrInvalidDaemonStatus)
	require.Len(t, calls, 2)
	require.Equal(t, []string{"--app-name", "custom", "daemon", "status", "--json"}, calls[1].Args)
}

func TestCLIFallbackRecoversAndDoesNotRetryStartedCommands(t *testing.T) {
	var calls []process.Command
	failure := errors.New("started operation failed")
	client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		calls = append(calls, cmd)
		if cmd.Name == "sbx" {
			return "", exec.ErrNotFound
		}
		return "failure output", failure
	}})
	_, err := client.Version(context.Background())
	require.ErrorIs(t, err, exec.ErrNotFound)
	arm := installCandidate(t, client, 0)
	installCandidate(t, client, 1)
	_, err = client.WorkspaceDirectory(context.Background(), "demo")
	require.ErrorIs(t, err, failure)
	require.Len(t, calls, 3)
	require.Equal(t, arm, calls[2].Name)
	require.Equal(t, []string{"exec", "demo", "--", "pwd", "-P"}, calls[2].Args)
	require.Equal(t, []string{"SSH_AUTH_SOCK"}, calls[2].Unset)
}

func TestInteractiveCLIFallbackPreservesAppScope(t *testing.T) {
	var calls []string
	failure := errors.New("started setup failed")
	client := discoveryClient(t, process.Runner{Interactive: func(_ context.Context, name string, args ...string) error {
		calls = append(calls, name)
		require.Equal(t, []string{"--app-name", "custom", "setup", "ssh"}, args)
		if name == "sbx" {
			return exec.ErrNotFound
		}
		return failure
	}})
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "custom")
	arm := installCandidate(t, client, 0)
	installCandidate(t, client, 1)
	require.ErrorIs(t, client.SetupSSHInteractive(context.Background()), failure)
	require.Equal(t, []string{"sbx", arm}, calls)
}

func TestNonMacOSDoesNotDiscover(t *testing.T) {
	client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		require.Equal(t, "sbx", cmd.Name)
		return "", exec.ErrNotFound
	}})
	client.discovery.platform = "linux"
	installCandidate(t, client, 0)
	client.transport = daemonTestTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected socket probe"); return nil, nil })
	_, err := client.DaemonStatus(context.Background())
	require.ErrorIs(t, err, exec.ErrNotFound)
	_, err = client.Version(context.Background())
	require.ErrorIs(t, err, exec.ErrNotFound)
}

func TestSocketDiscoveryUsesBoundedCallerContext(t *testing.T) {
	client := discoveryClient(t, process.Runner{Execute: func(context.Context, process.Command) (string, error) { return "", exec.ErrNotFound }})
	installCandidate(t, client, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.transport = daemonTestTransport(func(r *http.Request) (*http.Response, error) {
		_, bounded := r.Context().Deadline()
		require.True(t, bounded)
		cancel()
		require.ErrorIs(t, r.Context().Err(), context.Canceled)
		return nil, r.Context().Err()
	})
	_, err := client.DaemonStatus(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestHomebrewDaemonCommandFailureIsNotRetried(t *testing.T) {
	calls := 0
	failure := errors.New("daemon status failed")
	client := discoveryClient(t, process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		calls++
		if cmd.Name == "sbx" {
			return "", exec.ErrNotFound
		}
		return "daemon diagnostic", failure
	}})
	installCandidate(t, client, 0)
	installCandidate(t, client, 1)
	client.transport = inventoryTransport("", 0, os.ErrNotExist)
	_, err := client.DaemonStatus(context.Background())
	require.ErrorIs(t, err, failure)
	require.Equal(t, "daemon diagnostic", CommandOutput(err))
	require.Equal(t, 2, calls)
}
