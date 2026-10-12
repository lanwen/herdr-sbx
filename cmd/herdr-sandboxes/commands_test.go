package main

import (
	"bytes"
	"context"
	"github.com/lanwen/herdr-sbx/internal/store"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandHelpDoesNotRequireExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	names := []string{""}
	for _, command := range commands().Commands {
		names = append(names, command.Name)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			command := commands()
			var output bytes.Buffer
			command.Writer, command.ErrWriter = &output, &output
			args := []string{"herdr-sandboxes"}
			if name != "" {
				args = append(args, name)
			}
			args = append(args, "--help")
			if err := command.Run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "USAGE:") || !strings.Contains(output.String(), "herdr-sandboxes") {
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
		{[]string{"status", "extra"}, "usage: herdr-sandboxes status"},
		{[]string{"--unknown-flag"}, ""},
		{[]string{"repair"}, "usage: herdr-sandboxes repair"},
		{[]string{"repair", "daemon", "extra"}, "usage: herdr-sandboxes repair"},
		{[]string{"repair", "unsupported"}, "unknown repair action"},
		{[]string{"retry-preparation"}, "usage: herdr-sandboxes retry-preparation"},
		{[]string{"retry-preparation", "demo", "extra"}, "usage: herdr-sandboxes retry-preparation"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			command := commands()
			command.Writer, command.ErrWriter = io.Discard, io.Discard
			err := command.Run(context.Background(), append([]string{"herdr-sandboxes"}, test.args...))
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
		err := cmd.Run(context.Background(), []string{"herdr-sandboxes", "status"})
		return output.String(), err
	}
	output, err := run()
	if err != nil || !strings.Contains(output, "Worker: STOPPED") {
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
		if err != nil || !strings.Contains(output, "Worker: "+strings.ToUpper(state)) || !strings.Contains(output, "connection detail") || strings.Contains(output, "not yet") {
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
