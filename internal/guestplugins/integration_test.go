package guestplugins

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
func TestReviewrRealServer(t *testing.T) {
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
	os.WriteFile(filepath.Join(plugin, "herdr-plugin.toml"), []byte(`id="persiyanov.reviewr"
name="reviewr"
version="0.36.2"
min_herdr_version="0.9.1"
[[build]]
command=["false"]
[[actions]]
id="toggle"
title="Toggle"
command=["bash","herdr/pane.sh"]
contexts=["pane","workspace"]
`), 0600)
	os.WriteFile(filepath.Join(plugin, "herdr", "pane.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	lastRoot := filepath.Join(root, "last")
	os.MkdirAll(lastRoot, 0700)
	os.WriteFile(filepath.Join(lastRoot, "herdr-plugin.toml"), []byte(`id="herdr-last"
name="last"
version="0.1.0"
min_herdr_version="0.9.1"
[[actions]]
id="toggle-tab"
title="Last tab"
command=["./bin/herdr-last","toggle-tab"]
`), 0600)
	localJSON, _ := json.Marshal([]map[string]any{{"plugin_id": reviewrID, "version": "0.36.2", "enabled": true, "plugin_root": plugin}, {"plugin_id": "herdr-last", "version": "0.1.0", "enabled": false, "plugin_root": lastRoot}})
	raceConfig := false
	shell := func(ctx context.Context, command process.Command) (string, error) {
		if command.Name != "ssh" {
			return string(localJSON), nil
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
	if err := store.SetGuestPluginDefault(reviewrID, true, nil); err != nil {
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
	archive := archiveFixture(t, "herdr-reviewr", 0)
	downloads := 0
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		downloads++
		data := archive
		if strings.HasSuffix(req.URL.Path, "herdr-last-linux-arm64") {
			data = []byte("last binary")
		}
		if strings.HasSuffix(req.URL.Path, "SHA256SUMS") {
			data = []byte(digest([]byte("last binary")) + "  herdr-last-linux-arm64\n")
		}
		if strings.HasSuffix(req.URL.Path, ".sha256") {
			data = []byte(digest(archive))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	for range 2 {
		if err := Apply(ctx, client, transport, "fixture.sbx", func() bool { return true }); err != nil {
			t.Fatal(err)
		}
	}
	if downloads != 2 {
		t.Fatalf("repeat provisioning downloaded again: %d", downloads)
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
	backup, err := os.ReadFile(guestConfig + ".herdr-sbx-persiyanov.reviewr.bak")
	if err != nil || !strings.Contains(string(backup), "# guest settings") {
		t.Fatalf("missing original backup: %v", err)
	}
	// Local installation does not opt a plugin into guests.
	absent, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "herdr-last")
	if err != nil || absent.Plugin.ID != "" {
		t.Fatalf("new plugin was auto-enabled: %+v %v", absent.Plugin, err)
	}
	if err := store.SetGuestPluginDefault("herdr-last", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, client, transport, "fixture.sbx", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	last, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "herdr-last")
	if err != nil || !last.Plugin.Enabled {
		t.Fatal("second plugin was not applied", err)
	}
	if err := store.SetGuestPluginDefault("herdr-last", false, nil); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, client, transport, "fixture.sbx", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	last, err = client.InspectGuestPlugin(ctx, "fixture.sbx", "herdr-last")
	if err != nil || !last.Plugin.Enabled {
		t.Fatal("opt-out changed existing guest plugin", err)
	}

	// A prepared plugin without a built-in release recipe uses the same path.
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
	if err := store.SetGuestPluginBundle("example.portable", portable); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGuestPluginDefault("example.portable", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, client, transport, "fixture.sbx", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	generic, err := client.InspectGuestPlugin(ctx, "fixture.sbx", "example.portable")
	if err != nil || !generic.Plugin.Enabled {
		t.Fatal("generic bundle was not applied", err)
	}
	if err := store.SetGuestPluginDefault("example.portable", false, nil); err != nil {
		t.Fatal(err)
	}

	raceConfig = true
	if err := os.WriteFile(hostConfig, []byte(strings.ReplaceAll(hostKeys, "prefix+ctrl+r", "prefix+ctrl+t")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, client, transport, "fixture.sbx", func() bool { return true }); err == nil || !strings.Contains(err.Error(), "config changed") {
		t.Fatalf("concurrent edit was not rejected: %v", err)
	}
	data, err = os.ReadFile(guestConfig)
	if err != nil || !strings.Contains(string(data), "# concurrent edit") {
		t.Fatal("overwrote concurrent configuration")
	}
}
