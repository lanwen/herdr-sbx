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
			if err := store.SaveState(st); err != nil {
				t.Fatal(err)
			}
			got := f.service.projectReadiness(context.Background(), "scope", &st, sbx.Sandbox{ID: "sb1", Name: "demo", Status: "running"}, &item, true, func() bool { return true })
			if got.Status != "workspace setup required" || !strings.Contains(got.Detail, tc.detail) {
				t.Fatalf("outcome=%#v", got)
			}
			if err := saveResultError("scope/sb1", got); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadStatusStore()
			if err != nil {
				t.Fatal(err)
			}
			if len(status.Errors) != 1 {
				t.Fatalf("diagnostics=%#v", status.Errors)
			}
			for _, entry := range status.Errors {
				if entry.Code != tc.code {
					t.Fatalf("code=%s want=%s", entry.Code, tc.code)
				}
			}
			saved, err := store.LoadState()
			if err != nil || !reflect.DeepEqual(saved.Scopes, st.Scopes) || !reflect.DeepEqual(item, st.Scopes["scope"][0]) {
				t.Fatalf("ownership or workspace changed: saved=%#v item=%#v err=%v", saved, item, err)
			}
			if err := saveResultError("scope/sb1", result{Status: "ready"}); err != nil {
				t.Fatal(err)
			}
			status, err = store.ReadStatusStore()
			if err != nil || len(status.Errors) != 0 {
				t.Fatalf("recovery did not clear diagnostic: errors=%#v err=%v", status.Errors, err)
			}
		})
	}
}
