package registration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestOwnedGuestObservationDoesNotRepeatUnchangedWork(t *testing.T) {
	for _, connectionFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("connection failure=%t", connectionFails), func(t *testing.T) {
			originalPath := os.Getenv("PATH")
			f := newRegistrationFixture(t, "[]")
			t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
			root := t.TempDir()
			manifest := "id='example.plugin'\nversion='1.0.0'\n"
			path := filepath.Join(root, "herdr-plugin.toml")
			if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			local := herdr.PluginInstall{ID: "example.plugin", Version: "1.0.0", Root: root, ManifestPath: path}
			catalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{local}}})
			revision := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("1.0.0aarch64herdr-plugin.toml:600:%x\n", sha256.Sum256([]byte(manifest))))))[:24]
			guest := local
			guest.Root = "/home/guest/.local/share/herdr-sbx/plugins/example.plugin-" + revision
			guest.Enabled, guest.Available = true, true
			guestCatalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{guest}}})
			_, connectionError := exec.Command("sh", "-c", "exit 255").Output()
			inspections, readiness := 0, 0
			f.runner.Execute = func(_ context.Context, cmd process.Command) (string, error) {
				if cmd.Name == "herdr" && strings.Contains(strings.Join(cmd.Args, " "), "status server") {
					readiness++
					return "", fmt.Errorf("fresh status must not run")
				}
				if cmd.Name == "herdr" {
					return string(catalog), nil
				}
				if cmd.Name == "ssh" {
					if strings.Contains(strings.Join(cmd.Args, " "), "status server --json") {
						return `{"running":true,"version":"0.9.3"}`, nil
					}
					inspections++
					if connectionFails {
						return `workspace directory "/missing/host/project" no longer exists on the host; remove the sandbox with sbx rm demo`, connectionError
					}
					return "HERDR_SBX_PLUGIN_V1_BEGIN\nLinux aarch64\n" + base64.StdEncoding.EncodeToString([]byte("/home/guest/config.toml")) + "\nabsent\n\n" + base64.StdEncoding.EncodeToString(guestCatalog) + "\nmanaged\n" + fmt.Sprintf("%x", sha256.Sum256(nil)) + "\nHERDR_SBX_PLUGIN_V1_END\n", nil
				}
				return "", fmt.Errorf("unexpected command %s", cmd.Name)
			}
			f.rebuildClients()
			if err := store.SetGuestPluginDefault(local.ID, true); err != nil {
				t.Fatal(err)
			}
			item := store.Tracked{SandboxID: "sandbox", Target: "demo.sbx", ProfileID: "machine", ProjectWorkspaceDone: true}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
			if err := store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			sandbox := sbx.Sandbox{ID: "sandbox", Name: "demo", Status: "running"}
			for range 3 {
				got := f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
				if connectionFails && (got.Status == "failed" || !strings.Contains(got.Detail, "plugin")) {
					t.Fatalf("misleading registration failure: %+v", got)
				}
				if connectionFails {
					if len(got.PluginFailures) != 1 {
						t.Fatalf("missing actionable plugin connection cause: %+v", got)
					}
					assertMissingWorkspaceSyncFailure(t, got.PluginFailures[0])
					if err := store.SetGuestPluginFailures("scope/sandbox", got.PluginFailures); err != nil {
						t.Fatal(err)
					}
				}
				if !connectionFails && got.Status != "ready" {
					t.Fatalf("not ready: %+v", got)
				}
			}
			if readiness != 0 || inspections != 1 {
				t.Fatalf("unchanged owned registration repeated work: readiness=%d inspections=%d", readiness, inspections)
			}
			if connectionFails {
				journal, err := store.ReadStatusStore()
				if err != nil {
					t.Fatal(err)
				}
				for _, failure := range journal.Errors {
					if failure.Count != 1 {
						t.Fatalf("cached observation counted as another failed attempt: %+v", failure)
					}
				}
			}
			// A new process uses the durable observation too.
			state, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			item = state.Scopes["scope"][0]
			f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
			if inspections != 1 {
				t.Fatal("daemon restart repeated unchanged guest inspection")
			}
			for _, ctx := range []context.Context{WithFreshGuestObservation(context.Background()), context.WithValue(context.Background(), guestPluginRetryKey{}, true)} {
				before := inspections
				f.service.projectReadiness(ctx, "scope", &state, sandbox, &item, true, func() bool { return true })
				if inspections != before+1 {
					t.Fatalf("explicit or lifecycle refresh did not inspect guest: %d -> %d", before, inspections)
				}
			}
			item.GuestSync.CheckedAt = time.Now().Add(-16 * time.Minute)
			before := inspections
			f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
			if inspections != before+1 {
				t.Fatal("bounded audit did not inspect guest")
			}
			if connectionFails {
				if err := os.WriteFile(filepath.Join(root, "source.sh"), []byte("new source"), 0600); err != nil {
					t.Fatal(err)
				}
				before = inspections
				f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
				if inspections != before+1 {
					t.Fatal("source change did not invalidate guest observation")
				}
				if err := store.SetGuestPluginDefault(local.ID, false); err != nil {
					t.Fatal(err)
				}
				got := f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
				if inspections != before+1 || got.Status != "ready" {
					t.Fatalf("Off must clear previous sync failure without contacting guest: %+v", got)
				}
				if err := store.SetGuestPluginDefault(local.ID, true); err != nil {
					t.Fatal(err)
				}
				f.service.projectReadiness(context.Background(), "scope", &state, sandbox, &item, true, func() bool { return true })
				if inspections != before+2 {
					t.Fatal("re-enabling synchronization did not inspect guest")
				}
			}
			if !item.ProjectWorkspaceDone || item.ProfileID != "machine" {
				t.Fatalf("ownership/workspace lost: %+v", item)
			}
		})
	}
}

func TestOwnedMachineWithTwoEnabledPluginsKeepsIndependentConnectionFailures(t *testing.T) {
	for _, changeDuringWork := range []bool{false, true} {
		t.Run(fmt.Sprintf("source change during work=%t", changeDuringWork), func(t *testing.T) {
			originalPath := os.Getenv("PATH")
			f := newRegistrationFixture(t, "[]")
			t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
			plugins := []herdr.PluginInstall{}
			for _, id := range []string{"example.first", "example.second"} {
				root := t.TempDir()
				manifest := filepath.Join(root, "herdr-plugin.toml")
				if err := os.WriteFile(manifest, []byte(fmt.Sprintf("id='%s'\nversion='1.0.0'\n", id)), 0600); err != nil {
					t.Fatal(err)
				}
				plugins = append(plugins, herdr.PluginInstall{ID: id, Version: "1.0.0", Root: root, ManifestPath: manifest})
				if err := store.SetGuestPluginDefault(id, true); err != nil {
					t.Fatal(err)
				}
			}
			catalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": plugins}})
			_, connectionError := exec.Command("sh", "-c", "exit 255").Output()
			inspections := 0
			f.runner.Execute = func(_ context.Context, cmd process.Command) (string, error) {
				if cmd.Name == "herdr" && strings.Join(cmd.Args, " ") == "plugin list --json" {
					return string(catalog), nil
				}
				if cmd.Name == "ssh" {
					if strings.Contains(strings.Join(cmd.Args, " "), "status server --json") {
						return `{"running":true,"version":"0.9.3"}`, nil
					}
					inspections++
					if changeDuringWork && inspections == 1 {
						if err := os.WriteFile(filepath.Join(plugins[0].Root, "changed-source"), []byte("changed during guest work"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return `workspace directory "/missing/host/project" no longer exists on the host; remove the sandbox with sbx rm demo`, connectionError
				}
				t.Fatalf("completed registration ran unexpected work: %s %v", cmd.Name, cmd.Args)
				return "", nil
			}
			f.rebuildClients()
			item := store.Tracked{SandboxID: "sandbox", Target: "demo.sbx", ProfileID: "machine", ProjectWorkspaceDone: true}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
			for pass := range 3 {
				got := f.service.projectReadiness(context.Background(), "scope", &state, sbx.Sandbox{ID: "sandbox", Name: "demo", Status: "running"}, &item, true, func() bool { return true })
				if changeDuringWork && pass == 0 && item.GuestSync != nil {
					t.Fatal("source changed during work but observation was cached")
				}
				if len(got.PluginFailures) != 2 || got.Status != "plugin setup required" {
					t.Fatalf("missing independent plugin failures: %+v", got)
				}
				for _, failure := range got.PluginFailures {
					assertMissingWorkspaceSyncFailure(t, failure)
				}
				if err := store.SetGuestPluginFailures("scope/sandbox", got.PluginFailures); err != nil {
					t.Fatal(err)
				}
			}
			journal, err := store.ReadStatusStore()
			if err != nil {
				t.Fatal(err)
			}
			expectedPasses := 1
			if changeDuringWork {
				expectedPasses = 2
			}
			if inspections != 2*expectedPasses || len(journal.Errors) != 2 {
				t.Fatalf("repeated guest work or mixed plugin diagnostics: inspections=%d errors=%d", inspections, len(journal.Errors))
			}
			for _, failure := range journal.Errors {
				if failure.Count != expectedPasses {
					t.Fatalf("cached failure counted repeatedly: %+v", failure)
				}
			}
			saved, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if got := saved.Scopes["scope"][0]; got.ProfileID != "machine" || !got.ProjectWorkspaceDone {
				t.Fatalf("lost completed registration: %+v", got)
			}

		})
	}
}

func assertMissingWorkspaceSyncFailure(t *testing.T, failure store.PluginFailure) {
	t.Helper()
	if failure.Code != store.CodeGuestPluginSync {
		t.Fatalf("unexpected connection failure code: %+v", failure)
	}
	for _, part := range []string{"herdr-sbx was trying to inspect this herdr plugin", "sbx refused a new SSH connection before any guest work", "/missing/host/project", "no longer exists"} {
		if !strings.Contains(failure.Detail, part) {
			t.Fatalf("missing connection action/cause %q: %+v", part, failure)
		}
	}
	for _, part := range []string{"herdr machine and existing workspaces were kept", "restore the saved host workspace path", "Re-sync"} {
		if !strings.Contains(failure.Recovery, part) {
			t.Fatalf("missing recovery guidance %q: %+v", part, failure)
		}
	}
	text := failure.Detail + "\n" + failure.Recovery
	for _, unwanted := range []string{"sbx rm", "inspect guest plugin:", "guest plugin inspection could not", "Error: Custom", "exit status"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("connection explanation retained raw/destructive diagnostic %q: %+v", unwanted, failure)
		}
	}
}
