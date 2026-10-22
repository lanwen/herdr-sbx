package guestplugins

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
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
			require.ErrorIs(t, err, cause, "lost inspection cause: %v", err)
			failure := FailureFor(herdr.PluginInstall{ID: "example.plugin"}, "revision", err)
			require.Equal(t, tc.want, failure.Detail, "detail:\n%s\nwant:\n%s", failure.Detail, tc.want)
			require.NotContains(t, failure.Detail+failure.Recovery+failure.Output, "sbx rm")
			require.NotContains(t, failure.Detail, "private")
			require.EqualValues(t, "", failure.Command)
			require.Contains(t, failure.Recovery, "Re-sync")
			require.Contains(t, failure.Recovery, "kept")
			require.False(t, tc.name == "missing host workspace" && (strings.Count(failure.Detail, "/Users/test/project/sbxenv.yaml") != 1 || !strings.Contains(failure.Recovery, "restore")), "missing workspace recovery: %#v", failure)
		})
	}
}
