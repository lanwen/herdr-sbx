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
	"github.com/stretchr/testify/require"
)

func TestOwnedGuestObservationDoesNotRepeatUnchangedWork(t *testing.T) {
	for _, connectionFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("connection failure=%t", connectionFails), func(t *testing.T) {
			testOwnedGuestObservationDoesNotRepeatUnchangedWork(t, connectionFails)
		})
	}
}

func TestOwnedMachineWithTwoEnabledPluginsKeepsIndependentConnectionFailures(t *testing.T) {
	for _, changeDuringWork := range []bool{false, true} {
		t.Run(fmt.Sprintf("source change during work=%t", changeDuringWork), func(t *testing.T) {
			testOwnedMachineWithTwoEnabledPluginsKeepsIndependentConnectionFailures(t, changeDuringWork)
		})
	}
}

func assertMissingWorkspaceSyncFailure(t *testing.T, failure store.PluginFailure) {
	t.Helper()
	require.Equal(t, store.CodeGuestPluginSync, failure.Code, "unexpected connection failure code: %+v", failure)
	for _, part := range []string{"herdr-sbx was trying to inspect this herdr plugin", "sbx refused a new SSH connection before any guest work", "/missing/host/project", "no longer exists"} {
		require.Contains(t, failure.Detail, part, "missing connection action/cause %q: %+v", part, failure)
	}
	for _, part := range []string{"herdr machine and existing workspaces were kept", "restore the saved host workspace path", "Re-sync"} {
		require.Contains(t, failure.Recovery, part, "missing recovery guidance %q: %+v", part, failure)
	}
	text := failure.Detail + "\n" + failure.Recovery
	for _, unwanted := range []string{"sbx rm", "inspect guest plugin:", "guest plugin inspection could not", "Error: Custom", "exit status"} {
		require.NotContains(t, text, unwanted, "connection explanation retained raw/destructive diagnostic %q: %+v", unwanted, failure)
	}
}

func testOwnedGuestObservationDoesNotRepeatUnchangedWork(t *testing.T, connectionFails bool) {
	t.Helper()

	originalPath := os.Getenv("PATH")
	f := newRegistrationFixture(t, "[]")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
	root := t.TempDir()
	manifest := "id='example.plugin'\nversion='1.0.0'\n"
	path := filepath.Join(root, "herdr-plugin.toml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0600))
	local := herdr.PluginInstall{ID: "example.plugin", Version: "1.0.0", Root: root, ManifestPath: path}
	catalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{local}}})
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("1.0.0aarch64herdr-plugin.toml:600:%x\n", sha256.Sum256([]byte(manifest))))))[:24]
	guest := local
	guest.Root = "/home/guest/.local/share/herdr-sbx/plugins/example.plugin-" + revision
	guest.Enabled, guest.Available = true, true
	guestCatalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{guest}}})
	_, connectionError := exec.Command("sh", "-c", "exit 255").Output()
	commands := &observationCommands{catalog: catalog, guestCatalog: guestCatalog, connectionFails: connectionFails, connectionError: connectionError}
	f.runner.Execute = commands.execute
	f.rebuildClients()
	require.NoError(t, store.SetGuestPluginDefault(local.ID, true))
	item := store.Tracked{SandboxID: "sandbox", Target: "demo.sbx", ProfileID: "machine", ProjectWorkspaceDone: true}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	require.NoError(t, store.SaveState(state))
	sandbox := sbx.Sandbox{ID: "sandbox", Name: "demo", Status: "running"}
	for range 3 {
		got := f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.False(t, connectionFails && (got.Status == "failed" || !strings.Contains(got.Detail, "plugin")), "misleading registration failure: %+v", got)
		if connectionFails {
			require.Len(t, got.PluginFailures, 1, "missing actionable plugin connection cause: %+v", got)
			assertMissingWorkspaceSyncFailure(t, got.PluginFailures[0])
			require.NoError(t, store.SetGuestPluginFailures("scope/sandbox", got.PluginFailures))
		}
		require.False(t, !connectionFails && got.Status != "ready", "not ready: %+v", got)
	}
	require.EqualValues(t, 0, commands.readiness)
	require.EqualValues(t, 1, commands.inspections)
	if connectionFails {
		assertCachedFailureCount(t)
	}
	// A new process uses the durable observation too.
	state, err := store.LoadState()
	require.NoError(t, err)
	item = state.Scopes["scope"][0]
	f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
	require.Equal(t, 1, commands.inspections, "daemon restart repeated unchanged guest inspection")
	for _, ctx := range []context.Context{WithFreshGuestObservation(context.Background()), context.WithValue(context.Background(), guestPluginRetryKey{}, true)} {
		before := commands.inspections
		f.service.projectReadiness(ctx, registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.Equal(t, before+1, commands.inspections, "explicit or lifecycle refresh did not inspect guest: %d -> %d", before, commands.inspections)
	}
	item.GuestSync.CheckedAt = time.Now().Add(-16 * time.Minute)
	before := commands.inspections
	f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
	require.Equal(t, before+1, commands.inspections, "bounded audit did not inspect guest")
	if connectionFails {
		require.NoError(t, os.WriteFile(filepath.Join(root, "source.sh"), []byte("new source"), 0600))
		before = commands.inspections
		f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.Equal(t, before+1, commands.inspections, "source change did not invalidate guest observation")
		require.NoError(t, store.SetGuestPluginDefault(local.ID, false))
		got := f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.False(t, commands.inspections != before+1)
		require.EqualValues(t, "ready", got.Status)
		require.NoError(t, store.SetGuestPluginDefault(local.ID, true))
		f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.Equal(t, before+2, commands.inspections, "re-enabling synchronization did not inspect guest")
	}
	require.True(t, item.ProjectWorkspaceDone)
	require.EqualValues(t, "machine", item.ProfileID)
}

func testOwnedMachineWithTwoEnabledPluginsKeepsIndependentConnectionFailures(t *testing.T, changeDuringWork bool) {
	t.Helper()

	originalPath := os.Getenv("PATH")
	f := newRegistrationFixture(t, "[]")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+originalPath)
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
	plugins := []herdr.PluginInstall{}
	for _, id := range []string{"example.first", "example.second"} {
		root := t.TempDir()
		manifest := filepath.Join(root, "herdr-plugin.toml")
		require.NoError(t, os.WriteFile(manifest, []byte(fmt.Sprintf("id='%s'\nversion='1.0.0'\n", id)), 0600))
		plugins = append(plugins, herdr.PluginInstall{ID: id, Version: "1.0.0", Root: root, ManifestPath: manifest})
		require.NoError(t, store.SetGuestPluginDefault(id, true))
	}
	catalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": plugins}})
	_, connectionError := exec.Command("sh", "-c", "exit 255").Output()
	commands := &multiObservationCommands{t: t, catalog: catalog, plugins: plugins, changeDuringWork: changeDuringWork, connectionError: connectionError}
	f.runner.Execute = commands.execute
	f.rebuildClients()
	item := store.Tracked{SandboxID: "sandbox", Target: "demo.sbx", ProfileID: "machine", ProjectWorkspaceDone: true}
	state := store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {item}}}
	for pass := range 3 {
		got := f.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sbx.Sandbox{ID: "sandbox", Name: "demo", Status: "running"}, Current: func() bool { return true }}, &item, true)
		require.False(t, changeDuringWork && pass == 0 && item.GuestSync != nil, "source changed during work but observation was cached")
		require.Len(t, got.PluginFailures, 2)
		require.EqualValues(t, "plugin setup required", got.Status)
		for _, failure := range got.PluginFailures {
			assertMissingWorkspaceSyncFailure(t, failure)
		}
		require.NoError(t, store.SetGuestPluginFailures("scope/sandbox", got.PluginFailures))
	}
	journal, err := store.ReadStatusStore()
	require.NoError(t, err)
	expectedPasses := 1
	if changeDuringWork {
		expectedPasses = 2
	}
	require.False(t, commands.inspections != 2*expectedPasses)
	require.Len(t, journal.Errors, 2)
	for _, failure := range journal.Errors {
		require.Equal(t, expectedPasses, failure.Count, "cached failure counted repeatedly: %+v", failure)
	}
	saved, err := store.LoadState()
	require.NoError(t, err)
	{
		got := saved.Scopes["scope"][0]
		require.EqualValues(t, "machine", got.ProfileID)
		require.True(t, got.ProjectWorkspaceDone)
	}
}

type observationCommands struct {
	catalog, guestCatalog  []byte
	connectionFails        bool
	connectionError        error
	inspections, readiness int
}

func (f *observationCommands) execute(_ context.Context, cmd process.Command) (string, error) {

	if cmd.Name == "herdr" && strings.Contains(strings.Join(cmd.Args, " "), "status server") {
		f.readiness++
		return "", fmt.Errorf("fresh status must not run")
	}
	if cmd.Name == "herdr" {
		return string(f.catalog), nil
	}
	if cmd.Name == "ssh" {
		if strings.Contains(strings.Join(cmd.Args, " "), "status server --json") {
			return `{"running":true,"version":"0.9.3"}`, nil
		}
		f.inspections++
		if f.connectionFails {
			return `workspace directory "/missing/host/project" no longer exists on the host; remove the sandbox with sbx rm demo`, f.connectionError
		}
		return "HERDR_SBX_PLUGIN_V1_BEGIN\nLinux aarch64\n" + base64.StdEncoding.EncodeToString([]byte("/home/guest/config.toml")) + "\nabsent\n\n" + base64.StdEncoding.EncodeToString(f.guestCatalog) + "\nmanaged\n" + fmt.Sprintf("%x", sha256.Sum256(nil)) + "\nHERDR_SBX_PLUGIN_V1_END\n", nil
	}
	return "", fmt.Errorf("unexpected command %s", cmd.Name)
}

type multiObservationCommands struct {
	t                *testing.T
	catalog          []byte
	plugins          []herdr.PluginInstall
	changeDuringWork bool
	connectionError  error
	inspections      int
}

func (f *multiObservationCommands) execute(_ context.Context, cmd process.Command) (string, error) {

	if cmd.Name == "herdr" && strings.Join(cmd.Args, " ") == "plugin list --json" {
		return string(f.catalog), nil
	}
	if cmd.Name == "ssh" {
		if strings.Contains(strings.Join(cmd.Args, " "), "status server --json") {
			return `{"running":true,"version":"0.9.3"}`, nil
		}
		f.inspections++
		if f.changeDuringWork && f.inspections == 1 {
			require.NoError(f.t, os.WriteFile(filepath.Join(f.plugins[0].Root, "changed-source"), []byte("changed during guest work"), 0600))
		}
		return `workspace directory "/missing/host/project" no longer exists on the host; remove the sandbox with sbx rm demo`, f.connectionError
	}
	f.t.Fatalf("completed registration ran unexpected work: %s %v", cmd.Name, cmd.Args)
	return "", nil
}

func assertCachedFailureCount(t *testing.T) {
	t.Helper()
	journal, err := store.ReadStatusStore()
	require.NoError(t, err)
	for _, failure := range journal.Errors {
		require.Equal(t, 1, failure.Count, "cached observation counted as another failed attempt: %+v", failure)
	}
}
