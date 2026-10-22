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
	"github.com/stretchr/testify/require"
)

// The SSH boundary is replaced with a local shell in a temporary HOME. All
// plugin linking and config reloads use a real, isolated Herdr server.
func TestInstalledPluginsRealServer(t *testing.T) {
	binary := os.Getenv("SBX_TEST_HERDR_BINARY")
	if binary == "" {
		t.Skip("set SBX_TEST_HERDR_BINARY for isolated Herdr integration")
	}
	root, err := os.MkdirTemp("/tmp", "sbx-plugin-")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	require.NoError(t, err)
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
	require.NoError(t, os.Symlink(binary, filepath.Join(bin, "herdr")))
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
			require.NoError(t, os.WriteFile(guestConfig, []byte("# concurrent edit\n[advanced]\nscrollback_limit_bytes=6000\n"), 0600))
		}
		script := strings.ReplaceAll(command.Args[len(command.Args)-1], "uname -sm", "printf 'Linux aarch64\\n'")
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		cmd.Env = append(os.Environ(), "HERDR_CONFIG_PATH="+guestConfig)
		cmd.Stdin = command.Stdin
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	require.NoError(t, store.SetGuestPluginDefault("example.actions", true))
	client := herdr.New(process.Runner{Execute: shell})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer func() {
		stop := exec.Command(binary, "--session", "default", "server", "stop")
		stop.Env = append(os.Environ(), "HERDR_CONFIG_PATH="+guestConfig)
		_ = stop.Run()
	}()
	require.NoError(t, client.StartProjectServer(ctx, "fixture.sbx", root))
	for range 2 {
		require.NoError(t, ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }))
	}
	require.Equal(t, 1, transfers, "unchanged plugin transferred again: %d transfers", transfers)
	installed, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.actions")
	require.NoError(t, err)
	{
		_, err := os.Stat(filepath.Join(installed.Plugin.Root, "actions.toml"))
		require.True(t, os.IsNotExist(err), "source manifest copied beside normalized guest manifest: %v", err)
	}
	{
		_, err := os.Stat(filepath.Join(installed.Plugin.Root, "herdr-plugin.toml"))
		require.NoError(t, err, "normalized guest manifest missing: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(installed.Plugin.Root, "herdr", "pane.sh"))
	require.NoError(t, err)
	require.EqualValues(t, "#!/bin/sh\nexit 0\n", string(content))
	data, err := os.ReadFile(guestConfig)
	require.NoError(t, err)
	doc, err := parseConfig(data)
	require.NoError(t, err)
	require.False(t, doc["advanced"].(map[string]any)["scrollback_limit_bytes"] != int64(5000), "lost guest setting")
	backup, err := os.ReadFile(guestConfig + ".herdr-sbx-example.actions.bak")
	require.NoError(t, err)
	require.Contains(t, string(backup), "# guest settings")
	// Local installation does not opt a plugin into guests.
	absent, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	require.NoError(t, err)
	require.EqualValues(t, "", absent.Plugin.ID)
	require.NoError(t, store.SetGuestPluginDefault("example.secondary", true))
	require.NoError(t, ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }))
	secondary, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	require.NoError(t, err)
	require.True(t, secondary.Plugin.Enabled)
	require.NoError(t, store.SetGuestPluginDefault("example.secondary", false))
	require.NoError(t, ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }))
	secondary, err = client.InspectGuestPlugin(ctx, "fixture.sbx", "example.secondary")
	require.NoError(t, err)
	require.True(t, secondary.Plugin.Enabled)

	// A portable plugin can use its installed files directly.
	require.NoError(t, store.SetGuestPluginDefault("example.portable", true))
	require.NoError(t, ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true }))
	generic, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.portable")
	require.NoError(t, err)
	require.True(t, generic.Plugin.Enabled)
	require.NoError(t, store.SetGuestPluginDefault("example.portable", false))

	raceConfig = true
	require.NoError(t, os.WriteFile(hostConfig, []byte(strings.ReplaceAll(hostKeys, "prefix+ctrl+r", "prefix+ctrl+t")), 0600))
	{
		err := ApplySandbox(ctx, client, "fixture.sbx", "fixture-scope", "fixture-sandbox", func() bool { return true })
		require.Error(t, err)
		require.Contains(t, err.Error(), "config changed")
	}
	data, err = os.ReadFile(guestConfig)
	require.NoError(t, err)
	require.Contains(t, string(data), "# concurrent edit")
}
