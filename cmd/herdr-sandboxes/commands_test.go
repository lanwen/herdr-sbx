package main

import (
	"bytes"
	"context"
	"io"
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
