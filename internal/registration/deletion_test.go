package registration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestSameNameReplacementNeverAuthorizesEntryDeletion(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	item := store.Tracked{SandboxID: "old", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}); err != nil {
		t.Fatal(err)
	}
	f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"uuid":"replacement","name":"demo","status":"running"}`))}, nil
	})
	f.rebuildClients()
	result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "old")
	if err == nil || !result.Retry || !strings.Contains(err.Error(), "another identity") {
		t.Fatalf("deletion=%+v err=%v", result, err)
	}
	for _, call := range *calls {
		if strings.Contains(call, "machine remove") {
			t.Fatalf("removed reused target: %s", call)
		}
	}
	state, _ := store.LoadState()
	if len(state.Scopes[scope]) != 1 || state.Scopes[scope][0].ProfileID != "machine" {
		t.Fatal("association lost")
	}
}

func TestConfirmedDeletionCleansDisabledEntryOnlyWhileManagementOn(t *testing.T) {
	f, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}}}}); err != nil {
		t.Fatal(err)
	}
	_ = store.SetEnabled(scope, "one", store.Disabled)
	f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"code":"sandbox_not_found"}`))}, nil
	})
	f.rebuildClients()
	_ = store.SetWorkerControl(true)
	if _, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one"); err == nil {
		t.Fatal("OFF allowed cleanup")
	}
	if len(*calls) != 0 {
		t.Fatalf("OFF issued cleanup commands: %v", *calls)
	}
	_ = store.SetWorkerControl(false)
	if _, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one"); err != nil {
		t.Fatal(err)
	}
	if len(*machines) != 0 {
		t.Fatal("confirmed deleted Disabled entry retained")
	}
}

func TestResumeRecreatesExternallyRemovedEnabledEntry(t *testing.T) {
	for _, runtime := range []string{"running", "stopped"} {
		t.Run(runtime, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, runtime)
			item := store.Tracked{SandboxID: "one", ProfileID: "removed-machine", Target: "demo.sbx", Label: "Old", ProjectWorkspaceDone: true}
			if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}); err != nil {
				t.Fatal(err)
			}
			_ = store.SetEnabled(scope, "one", store.Enabled)
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, c process.Command) (string, error) {
				call := c.Name + " " + strings.Join(c.Args, " ")
				switch call {
				case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
					return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
				case "herdr machine add --label sbx · demo demo.sbx":
					*calls = append(*calls, call)
					*machines = []herdr.Machine{{ID: "replacement-machine", Label: "sbx · demo", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
					return "Saved SSH machine replacement-machine. Remote server is ready.", nil
				}
				return execute(ctx, c)
			}
			f.rebuildClients()
			if err := f.service.CaptureBaseline(context.Background(), "/tmp/test-sandboxd.sock"); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := f.service.ReconcileContext(context.Background(), &output); err != nil {
				t.Fatal(err)
			}
			state, _ := store.LoadState()
			saved := state.Scopes[scope][0]
			if runtime == "running" {
				if saved.ProfileID != "replacement-machine" {
					t.Fatalf("not recreated: %+v\n%s", saved, output.String())
				}
			} else {
				if saved.ProfileID != "" || strings.Contains(output.String(), "cannot add a stopped sandbox") {
					t.Fatalf("stale association: %+v\n%s", saved, output.String())
				}
				for _, call := range *calls {
					if strings.Contains(call, "machine add") || strings.HasPrefix(call, "ssh -o") {
						t.Fatalf("stopped action connected: %s", call)
					}
				}
			}
		})
	}
}

func TestConfirmedDeletionRetainsCrossScopeAssociation(t *testing.T) {
	for _, sameProfile := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared profile", false: "shared target"}[sameProfile], func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "running")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			item := store.Tracked{SandboxID: "one", ProfileID: "machine", Target: "demo.sbx", Label: "Desk"}
			other := item
			if !sameProfile {
				other.ProfileID = "other-machine"
			}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}, "other-scope": {other}}}
			if err := store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"code":"sandbox_not_found"}`))}, nil
			})
			f.rebuildClients()
			result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
			if err != nil || !strings.Contains(result.Detail, "retained state") {
				t.Fatalf("deletion=%+v err=%v", result, err)
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine remove") {
					t.Fatalf("ambiguous cross-scope removal: %s", call)
				}
			}
			status, _ := store.ReadStatusStore()
			found := false
			for _, entry := range status.Errors {
				if entry.Scope == store.StatusKey(scope, "one") && entry.Code == store.CodeOwnershipConflict {
					found = true
				}
			}
			if !found {
				t.Fatal("association warning was not retained for inspection")
			}
			saved, _ := store.LoadState()
			if len(saved.Scopes[scope]) != 1 || len(saved.Scopes["other-scope"]) != 1 || len(*machines) != 1 {
				t.Fatal("ambiguous association was changed")
			}
		})
	}
}

func TestRemovedEntryDeletionReleasesIdentityForSameNameReplacement(t *testing.T) {
	for _, mode := range []string{"delete event", "missed deletion"} {
		t.Run(mode, func(t *testing.T) {
			f, scope, machines, calls := newManagementFixture(t, "stopped")
			*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(false)}}
			if err := f.service.SetEnabled(context.Background(), scope, "one", store.Disabled); err != nil {
				t.Fatal(err)
			}
			if err := f.service.RemoveEntry(context.Background(), scope, "one"); err != nil {
				t.Fatal(err)
			}
			// The old sandbox is gone before cleanup runs; its name is already reused.
			f.inventory = `[{"uuid":"replacement","name":"demo","status":"stopped"}]`
			f.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"uuid":"replacement","name":"demo","status":"stopped"}`
				if r.URL.Path == "/sandbox" {
					body = f.inventory
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			f.rebuildClients()
			*machines = []herdr.Machine{{ID: "replacement-entry", Label: "New desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
			*calls = nil
			if mode == "delete event" {
				result, err := f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
				if err != nil || result.Retry {
					t.Fatalf("cleanup=%+v err=%v", result, err)
				}
			} else {
				if err := f.service.ReconcileContext(context.Background(), io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			state, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range state.Scopes[scope] {
				if item.SandboxID == "one" {
					t.Fatalf("dead identity retained: %+v", item)
				}
			}
			rows, err := f.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
			if err != nil || len(rows) != 1 || rows[0].AssociationIssue != "" {
				t.Fatalf("replacement rows=%+v err=%v", rows, err)
			}
			if len(*machines) != 1 || (*machines)[0].ID != "replacement-entry" {
				t.Fatal("replacement native entry changed")
			}
			for _, call := range *calls {
				if strings.Contains(call, "machine remove") {
					t.Fatalf("cleanup removed a replacement entry: %s", call)
				}
			}
		})
	}
}

func TestLocalOnlyDeletionStillRequiresConfirmedIdentityAndManagement(t *testing.T) {
	for _, mode := range []string{"absent", "pending", "unknown", "present", "OFF"} {
		t.Run(mode, func(t *testing.T) {
			f, scope, _, calls := newManagementFixture(t, "stopped")
			item := store.Tracked{SandboxID: "one", Target: "demo.sbx", Pending: mode == "pending", ProjectWorkspaceDone: true}
			if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {item}}}); err != nil {
				t.Fatal(err)
			}
			f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				status, body := 404, `{"code":"sandbox_not_found"}`
				if mode == "unknown" {
					status, body = 503, `{}`
				}
				if mode == "present" {
					status, body = 200, `{"uuid":"one","name":"demo","status":"stopped"}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			f.rebuildClients()
			if mode == "OFF" {
				if err := store.SetWorkerControl(true); err != nil {
					t.Fatal(err)
				}
			}
			_, _ = f.service.RemoveDeletedSandbox(context.Background(), scope, "/tmp/test-sandboxd.sock", "one")
			state, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if (len(state.Scopes[scope]) == 0) != (mode == "absent") {
				t.Fatalf("mode %s lost/retained unexpected identity: %+v", mode, state)
			}
			if len(*calls) != 0 {
				t.Fatalf("local cleanup issued external command: %v", *calls)
			}
		})
	}
}
