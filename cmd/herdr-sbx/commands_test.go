package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/store"
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
			if err := command.Run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "USAGE:") || !strings.Contains(output.String(), "herdr-sbx") {
				t.Fatalf("missing generated help: %s", output.String())
			}
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
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want rejection containing %q", err, test.want)
			}
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
	if err != nil || path != wantPath {
		t.Fatalf("path=%s err=%v", path, err)
	}
	run := func() (string, error) {
		cmd := commands()
		var output bytes.Buffer
		cmd.Writer = &output
		err := cmd.Run(context.Background(), []string{"herdr-sbx", "status"})
		return output.String(), err
	}
	output, err := run()
	if err != nil || !strings.Contains(output, "Daemon: STOPPED") {
		t.Fatalf("output=%s err=%v", output, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("status created state: %v", err)
	}
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	for _, state := range []string{"running", "reconnecting"} {
		if err := store.RecordWorkerStatus(state, "connection detail", true); err != nil {
			t.Fatal(err)
		}
		output, err := run()
		if err != nil || !strings.Contains(output, "Daemon: "+strings.ToUpper(state)) || !strings.Contains(output, "connection detail") || strings.Contains(output, "not yet") {
			t.Fatalf("output=%s err=%v", output, err)
		}
	}
	if err := os.WriteFile(path+".status", []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("unreadable status succeeded")
	}
}

func TestDaemonCommandsPersistStopAndHonorAutomaticStart(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	run := func(args ...string) {
		t.Helper()
		cmd := commands()
		cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
		if err := cmd.Run(context.Background(), append([]string{"herdr-sbx", "daemon"}, args...)); err != nil {
			t.Fatal(err)
		}
	}
	run("stop")
	run("start", "--automatic")
	saved, err := store.ReadStatusStore()
	if err != nil || saved.WorkerControl == nil || !saved.WorkerControl.Stopped {
		t.Fatalf("automatic start changed saved Stop: %+v %v", saved.WorkerControl, err)
	}
	path, err := store.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".worker.log"); !os.IsNotExist(err) {
		t.Fatalf("automatic start launched stopped daemon: %v", err)
	}
	// The running daemon lock makes repeated explicit starts successful no-ops.
	if err := store.SetWorkerControl(false); err != nil {
		t.Fatal(err)
	}
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	run("start")
	run("start")
	saved, err = store.ReadStatusStore()
	if err != nil || saved.WorkerControl == nil || saved.WorkerControl.Stopped {
		t.Fatalf("explicit start not enabled: %+v %v", saved.WorkerControl, err)
	}
}

func TestDaemonCommandsRejectInvalidArguments(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(t.TempDir(), "absent"))
	for _, args := range [][]string{{}, {"unknown"}, {"start", "extra"}, {"stop", "extra"}, {"status", "extra"}} {
		cmd := commands()
		cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
		if err := cmd.Run(context.Background(), append([]string{"herdr-sbx", "daemon"}, args...)); err == nil {
			t.Fatalf("accepted daemon %v", args)
		}
	}
}

func TestDaemonStopReportsUnconfirmedExit(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	unlock, err := store.LockWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := store.RecordWorkerStatus("running", "", false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	cmd := commands()
	cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
	err = cmd.Run(ctx, []string{"herdr-sbx", "daemon", "stop"})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), store.CodeDaemonUnreachable) {
		t.Fatalf("unconfirmed daemon stop = %v", err)
	}
	saved, err := store.ReadStatusStore()
	if err != nil || saved.WorkerControl == nil || !saved.WorkerControl.Stopped {
		t.Fatalf("Stop was lost: %+v %v", saved.WorkerControl, err)
	}
	status, err := store.ReadWorkerStatus()
	if err != nil || status.State != "cancelling" {
		t.Fatalf("authoritative status=%+v error=%v", status, err)
	}
}
