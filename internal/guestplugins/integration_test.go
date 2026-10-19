package guestplugins

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// The SSH boundary is replaced with a local shell in a temporary HOME. All
// plugin linking and config reloads use a real, isolated Herdr server.
func TestInstalledPluginsRealServer(t *testing.T) {
	binary := os.Getenv("SBX_TEST_HERDR_BINARY")
	if binary == "" {
		t.Skip("set SBX_TEST_HERDR_BINARY for isolated Herdr integration")
	}
	root, err := os.MkdirTemp("/tmp", "sbx-plugin-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for key, dir := range map[string]string{"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_RUNTIME_DIR": "run"} {
		path := filepath.Join(root, dir)
		os.MkdirAll(path, 0700)
		t.Setenv(key, path)
	}
	for _, key := range []string{"HERDR_SOCKET_PATH", "HERDR_CLIENT_SOCKET_PATH", "HERDR_SESSION", "HERDR_STARTUP_CWD"} {
		t.Setenv(key, "")
	}
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0700)
	if err := os.Symlink(binary, filepath.Join(bin, "herdr")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin:/sbin:/usr/sbin")
	hostConfig := filepath.Join(root, "host.toml")
	guestConfig := filepath.Join(root, "guest.toml")
	os.WriteFile(hostConfig, []byte(hostKeys), 0600)
	os.WriteFile(guestConfig, []byte("# guest settings\n[advanced]\nscrollback_limit_bytes=5000\n"), 0600)
	t.Setenv("HERDR_CONFIG_PATH", hostConfig)
	plugin := filepath.Join(root, "plugin")
	os.MkdirAll(filepath.Join(plugin, "herdr"), 0700)
	os.WriteFile(filepath.Join(plugin, "actions.toml"), []byte(`id="example.actions"
name="Actions"
version="1.2.3"
min_herdr_version="0.9.1"
[[actions]]
id="toggle"
title="Toggle"
command=["bash","herdr/pane.sh"]
contexts=["pane","workspace"]
`), 0600)
	os.WriteFile(filepath.Join(plugin, "herdr", "pane.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	secondaryRoot := filepath.Join(root, "secondary")
	os.MkdirAll(secondaryRoot, 0700)
	os.WriteFile(filepath.Join(secondaryRoot, "herdr-plugin.toml"), []byte(`id="example.secondary"
name="Secondary"
version="0.1.0"
min_herdr_version="0.9.1"
[[actions]]
id="run"
title="Run secondary"
command=["sh","run.sh"]
`), 0600)
	os.WriteFile(filepath.Join(secondaryRoot, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	portable := filepath.Join(root, "portable")
	os.MkdirAll(portable, 0700)
	os.WriteFile(filepath.Join(portable, "herdr-plugin.toml"), []byte(`id="example.portable"
name="portable"
version="1.0.0"
min_herdr_version="0.9.1"
[[actions]]
id="run"
title="Run portable"
command=["sh","run.sh"]
`), 0600)
	os.WriteFile(filepath.Join(portable, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	localJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []map[string]any{{"plugin_id": "example.actions", "version": "1.2.3", "enabled": true, "plugin_root": plugin, "manifest_path": filepath.Join(plugin, "actions.toml")}, {"plugin_id": "example.secondary", "version": "0.1.0", "enabled": false, "plugin_root": secondaryRoot, "manifest_path": filepath.Join(secondaryRoot, "herdr-plugin.toml")}, {"plugin_id": "example.portable", "version": "1.0.0", "enabled": true, "plugin_root": portable, "manifest_path": filepath.Join(portable, "herdr-plugin.toml")}}}})
	transfers := 0
	raceConfig := false
	shell := func(ctx context.Context, command process.Command) (string, error) {
		if command.Name != "ssh" {
			return string(localJSON), nil
		}
		if command.Stdin != nil {
			transfers++
		}
		if command.Stdin != nil && raceConfig {
			if err := os.WriteFile(guestConfig, []byte("# concurrent edit\n[advanced]\nscrollback_limit_bytes=6000\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		script := strings.ReplaceAll(command.Args[len(command.Args)-1], "uname -sm", "printf 'Linux aarch64\\n'")
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		cmd.Env = append(os.Environ(), "HERDR_CONFIG_PATH="+guestConfig)
		cmd.Stdin = command.Stdin
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if err := store.SetGuestPluginDefault("example.actions", true); err != nil {
		t.Fatal(err)
	}
	client := herdr.New(process.Runner{Execute: shell})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		stop := exec.Command(binary, "--session", "default", "server", "stop")
		stop.Env = append(os.Environ(), "HERDR_CONFIG_PATH="+guestConfig)
		_ = stop.Run()
	}()
	if err := client.StartProjectServer(ctx, "fixture.sbx", root); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }); err != nil {
			t.Fatal(err)
		}
	}
	if transfers != 1 {
		t.Fatalf("unchanged plugin transferred again: %d transfers", transfers)
	}
	installed, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.actions")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(installed.Plugin.Root, "actions.toml")); !os.IsNotExist(err) {
		t.Fatalf("source manifest copied beside normalized guest manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed.Plugin.Root, "herdr-plugin.toml")); err != nil {
		t.Fatalf("normalized guest manifest missing: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(installed.Plugin.Root, "herdr", "pane.sh"))
	if err != nil || string(content) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("installed payload differs from local files: %q, %v", content, err)
	}
	data, err := os.ReadFile(guestConfig)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc["advanced"].(map[string]any)["scrollback_limit_bytes"] != int64(5000) {
		t.Fatal("lost guest setting")
	}
	backup, err := os.ReadFile(guestConfig + ".herdr-sbx-example.actions.bak")
	if err != nil || !strings.Contains(string(backup), "# guest settings") {
		t.Fatalf("missing original backup: %v", err)
	}
	// Local installation does not opt a plugin into guests.
	absent, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	if err != nil || absent.Plugin.ID != "" {
		t.Fatalf("new plugin was auto-enabled: %+v %v", absent.Plugin, err)
	}
	if err := store.SetGuestPluginDefault("example.secondary", true); err != nil {
		t.Fatal(err)
	}
	if err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	secondary, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	if err != nil || !secondary.Plugin.Enabled {
		t.Fatal("second plugin was not applied", err)
	}
	if err := store.SetGuestPluginDefault("example.secondary", false); err != nil {
		t.Fatal(err)
	}
	if err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	secondary, err = client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	if err != nil || !secondary.Plugin.Enabled {
		t.Fatal("opt-out changed existing guest plugin", err)
	}

	// A portable plugin can use its installed files directly.
	if err := store.SetGuestPluginDefault("example.portable", true); err != nil {
		t.Fatal(err)
	}
	if err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	generic, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.portable")
	if err != nil || !generic.Plugin.Enabled {
		t.Fatal("generic bundle was not applied", err)
	}
	if err := store.SetGuestPluginDefault("example.portable", false); err != nil {
		t.Fatal(err)
	}

	raceConfig = true
	if err := os.WriteFile(hostConfig, []byte(strings.ReplaceAll(hostKeys, "prefix+ctrl+r", "prefix+ctrl+t")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }); err == nil || !strings.Contains(err.Error(), "config changed") {
		t.Fatalf("concurrent edit was not rejected: %v", err)
	}
	data, err = os.ReadFile(guestConfig)
	if err != nil || !strings.Contains(string(data), "# concurrent edit") {
		t.Fatal("overwrote concurrent configuration")
	}
}
