package registration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestProjectWorkspaceHerdrFailures(t *testing.T) {
	const forwarding = "machine 'CM': remote binary discovery failed: remote herdr does not support machine API forwarding; update herdr on this machine"
	for _, tc := range []struct {
		name, output, code, detail string
	}{
		{"structured compatibility", `{"error":{"code":"unsupported","message":"` + forwarding + `"}}`, store.CodeHerdrCompatibility, "herdr in this sandbox could not provide machine API forwarding. Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."},
		{"rust compatibility", `Error: Custom { kind: Other, error: "` + forwarding + `" }`, store.CodeHerdrCompatibility, "herdr in this sandbox could not provide machine API forwarding. Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."},
		{"pinned herdr compatibility", `Error: Custom { kind: Other, error: "remote Herdr does not support machine API forwarding; update Herdr on this machine" }`, store.CodeHerdrCompatibility, "herdr in this sandbox could not provide machine API forwarding. Choose Fix herdr in Details to match the host release. If the installation cannot be replaced safely, update herdr inside the sandbox with its original installation method, then choose Re-sync."},
		{"ordinary herdr failure", `{"error":{"code":"invalid_cwd","message":"workspace is missing"}}`, store.CodeWorkspaceSetup, "herdr [invalid_cwd]: workspace is missing"},
		{"different versions", `{"error":{"code":"other","message":"local 0.9.3; remote 0.9.2"}}`, store.CodeWorkspaceSetup, "herdr [other]: local 0.9.3; remote 0.9.2"},
		{"malformed wrapper", `Error: Custom { kind: Other, error: "` + forwarding, store.CodeWorkspaceSetup, "inspect existing workspaces:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRegistrationFixture(t, "[]")
			f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				if command.Name == "herdr" && strings.Join(command.Args, " ") == "--machine machine-1 pane list" {
					return tc.output, errors.New("exit status 1")
				}
				return "", errors.New("unavailable observation")
			}
			f.rebuildClients()
			item := store.Tracked{SandboxID: "sb1", ProfileID: "machine-1", Label: "original label", Target: "demo.sbx", ProjectWorkspacePending: "/project"}
			st := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
			require.NoError(t, store.SaveState(st))
			got := f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &st, Sandbox: sbx.Sandbox{ID: "sb1", Name: "demo", Status: "running"}, Current: func() bool { return true }}, &item, true)
			require.EqualValues(t, "workspace setup required", got.Status)
			require.Contains(t, got.Detail, tc.detail)
			require.NoError(t, saveResultError("scope/sb1", got))
			status, err := store.ReadStatusStore()
			require.NoError(t, err)
			require.Len(t, status.Errors, 1, "diagnostics=%#v", status.Errors)
			for _, entry := range status.Errors {
				require.Equal(t, tc.code, entry.Code, "code=%s want=%s", entry.Code, tc.code)
			}
			saved, err := store.LoadState()
			require.NoError(t, err)
			require.True(t, reflect.DeepEqual(saved.Scopes, st.Scopes))
			require.True(t, reflect.DeepEqual(item, st.Scopes["scope"][0]))
			require.NoError(t, saveResultError("scope/sb1", result{Status: "ready"}))
			status, err = store.ReadStatusStore()
			require.NoError(t, err)
			require.Len(t, status.Errors, 0)
		})
	}
}
