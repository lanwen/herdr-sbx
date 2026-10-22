package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCommandHelpDoesNotRequireExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	names := []string{""}
	for _, command := range commands().Commands {
		names = append(names, command.Name)
		for _, subcommand := range command.Commands {
			names = append(names, command.Name+" "+subcommand.Name)
		}
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			command := commands()
			var output bytes.Buffer
			command.Writer, command.ErrWriter = &output, &output
			args := []string{"herdr-sbx"}
			if name != "" {
				args = append(args, strings.Fields(name)...)
			}
			args = append(args, "--help")
			require.NoError(t, command.Run(context.Background(), args))
			require.Contains(t, output.String(), "USAGE:")
			require.Contains(t, output.String(), "herdr-sbx")
		})
	}
}

func TestCommandRejectsInvalidArgumentsBeforeActions(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"unknown-command"}, ""},
		{[]string{"status", "extra"}, "usage: herdr-sbx status"},
		{[]string{"--unknown-flag"}, ""},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			command := commands()
			command.Writer, command.ErrWriter = io.Discard, io.Discard
			err := command.Run(context.Background(), append([]string{"herdr-sbx"}, test.args...))
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
		})
	}
}

func TestStatusCommandReadsWorkerWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	stateRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateRoot)
	wantPath := filepath.Join(stateRoot, "herdr", "plugins", "lanwen.herdr-sbx", "state.json")
	path, err := store.StatePath()
	require.NoError(t, err)
	require.False(t, path != wantPath)
	run := func() (string, error) {
		cmd := commands()
		var output bytes.Buffer
		cmd.Writer = &output
		err := cmd.Run(context.Background(), []string{"herdr-sbx", "status"})
		return output.String(), err
	}
	output, err := run()
	require.NoError(t, err)
	require.Contains(t, output, "Daemon: STOPPED")
	{
		_, err := os.Stat(filepath.Dir(path))
		require.True(t, os.IsNotExist(err), "status created state: %v", err)
	}
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	for _, state := range []string{"running", "reconnecting"} {
		require.NoError(t, store.RecordWorkerStatus(state, "connection detail", true))
		output, err := run()
		require.NoError(t, err)
		require.Contains(t, output, "Daemon: "+strings.ToUpper(state))
		require.Contains(t, output, "connection detail")
		require.NotContains(t, output, "not yet")
	}
	require.NoError(t, os.WriteFile(path+".status", []byte("invalid JSON"), 0600))
	{
		_, err := run()
		require.Error(t, err, "unreadable status succeeded")
	}
}

func TestDaemonCommandsPersistStopAndHonorAutomaticStart(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	run := func(args ...string) {
		t.Helper()
		cmd := commands()
		cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
		require.NoError(t, cmd.Run(context.Background(), append([]string{"herdr-sbx", "daemon"}, args...)))
	}
	run("stop")
	run("start", "--automatic")
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, saved.WorkerControl)
	require.True(t, saved.WorkerControl.Stopped)
	path, err := store.StatePath()
	require.NoError(t, err)
	{
		_, err := os.Stat(path + ".worker.log")
		require.True(t, os.IsNotExist(err), "automatic start launched stopped daemon: %v", err)
	}
	// The running daemon lock makes repeated explicit starts successful no-ops.
	require.NoError(t, store.SetWorkerControl(false))
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	run("start")
	run("start")
	saved, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, saved.WorkerControl)
	require.False(t, saved.WorkerControl.Stopped)
}

func TestDaemonCommandsRejectInvalidArguments(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(t.TempDir(), "absent"))
	for _, args := range [][]string{{}, {"unknown"}, {"start", "extra"}, {"stop", "extra"}, {"status", "extra"}} {
		cmd := commands()
		cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard

		err := cmd.Run(context.Background(), append([]string{"herdr-sbx", "daemon"}, args...))
		require.Error(t, err, "accepted daemon %v", args)
	}
}

func TestDaemonStopReportsUnconfirmedExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	unlock, err := store.LockWorker()
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, store.RecordWorkerStatus("running", "", false))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	cmd := commands()
	cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
	err = cmd.Run(ctx, []string{"herdr-sbx", "daemon", "stop"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), store.CodeDaemonUnreachable)
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotNil(t, saved.WorkerControl)
	require.True(t, saved.WorkerControl.Stopped)
	status, err := store.ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "cancelling", status.State)
}
