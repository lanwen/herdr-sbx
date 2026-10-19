package guestplugins

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestConnectionFailureExplainsPendingPluginInspection(t *testing.T) {
	for _, tc := range []struct {
		name, output, exit, want string
	}{
		{
			"missing host workspace",
			`workspace directory "/Users/test/project/sbxenv.yaml" no longer exists on the host; restore the directory, or remove the sandbox with: sbx rm demo`, "1",
			`herdr-sbx was trying to inspect this herdr plugin in the sandbox. sbx refused a new SSH connection before any guest work because the sandbox's saved host workspace path "/Users/test/project/sbxenv.yaml" no longer exists.`,
		},
		{
			"SSH refusal",
			"ssh: connect to host demo.sbx port 22: Connection refused\ntoken=private", "255",
			"herdr-sbx could not open a new SSH connection to inspect this herdr plugin in the sandbox: ssh: connect to host demo.sbx port 22: Connection refused\ntoken=[redacted]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := exec.Command("/bin/sh", "-c", "exit "+tc.exit).Run()
			client := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return tc.output, cause
			}})
			_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
			if !errors.Is(err, cause) {
				t.Fatalf("lost inspection cause: %v", err)
			}
			failure := FailureFor(herdr.PluginInstall{ID: "example.plugin"}, "revision", err)
			if failure.Detail != tc.want {
				t.Fatalf("detail:\n%s\nwant:\n%s", failure.Detail, tc.want)
			}
			if strings.Contains(failure.Detail+failure.Recovery+failure.Output, "sbx rm") || strings.Contains(failure.Detail, "private") || failure.Command != "" || !strings.Contains(failure.Recovery, "Re-sync") || !strings.Contains(failure.Recovery, "kept") {
				t.Fatalf("unsafe or misleading connection recovery: %#v", failure)
			}
			if tc.name == "missing host workspace" && (strings.Count(failure.Detail, "/Users/test/project/sbxenv.yaml") != 1 || !strings.Contains(failure.Recovery, "restore")) {
				t.Fatalf("missing workspace recovery: %#v", failure)
			}
		})
	}
}
