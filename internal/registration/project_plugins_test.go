package registration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

func TestProjectReadinessRetainsSeparatePluginAttempts(t *testing.T) {
	originalPath := os.Getenv("PATH")
	fixture := newRegistrationFixture(t, "[]")
	t.Setenv("PATH", originalPath)
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
	plugins := []herdr.PluginInstall{}
	for _, id := range []string{"example.first", "example.second"} {
		root := t.TempDir()
		manifest := filepath.Join(root, "herdr-plugin.toml")
		require.NoError(t, os.WriteFile(manifest, []byte(fmt.Sprintf("id=%q\nversion=\"1.0.0\"\n[[build]]\ncommand=[\"sh\",\"build.sh\"]\n", id)), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "build.sh"), []byte("exit 1\n"), 0700))
		plugins = append(plugins, herdr.PluginInstall{ID: id, Version: "1.0.0", Root: root, ManifestPath: manifest})
		require.NoError(t, store.SetGuestPluginDefault(id, true))
	}
	catalog, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": plugins}})
	commands := &pluginAttemptCommands{catalog: catalog}
	fixture.runner.Execute = commands.execute
	fixture.rebuildClients()
	item := store.Tracked{SandboxID: "id", ProfileID: "machine-1", Target: "demo.sbx"}
	state := store.State{Scopes: map[string][]store.Tracked{"scope": {item}}}
	sandbox := sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}
	var previous []store.PluginFailure
	for pass := range 2 {
		outcome := fixture.service.projectReadiness(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: sandbox, Current: func() bool { return true }}, &item, true)
		require.EqualValues(t, "manual intervention required", outcome.Status)
		require.Len(t, outcome.PluginFailures, 2)
		require.EqualValues(t, "2 herdr plugins need manual intervention", outcome.Detail)
		for i, failure := range outcome.PluginFailures {
			require.False(t, failure.PluginID != plugins[i].ID)
			require.False(t, failure.Output != []string{"build log for attempt 1: artifacts.example", "build log for attempt 2"}[i])
			require.NotEqualValues(t, "", failure.Recovery)
			require.False(t, failure.OccurredAt.IsZero())
			require.Equal(t, []string{store.CodeSandboxNetwork, store.CodeGuestPluginSync}[i], failure.Code, "plugin cause classification: %+v", failure)
			if pass == 1 {
				require.True(t, failure.OccurredAt.Equal(previous[i].OccurredAt))
				require.False(t, failure.Detail != previous[i].Detail)
			}
		}
		require.NoError(t, store.SetGuestPluginFailures(store.StatusKey("scope", "id"), outcome.PluginFailures))
		previous = outcome.PluginFailures
	}
	require.NoError(t, store.SetGuestPluginFailures(store.StatusKey("scope", "id"), previous[1:]))
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1, "independent recovery: %+v", status.Errors)
	for _, entry := range status.Errors {
		require.EqualValues(t, "example.second", entry.PluginID)
		require.EqualValues(t, 1, entry.Count)
	}
	require.Equal(t, 2, commands.transfers, "blocked attempt rebuilt: transfers=%d", commands.transfers)
}

type pluginAttemptCommands struct {
	catalog   []byte
	transfers int
}

func (f *pluginAttemptCommands) execute(_ context.Context, cmd process.Command) (string, error) {

	if cmd.Name == "herdr" {
		if strings.Contains(strings.Join(cmd.Args, " "), "status server") {
			return "running", nil
		}
		return string(f.catalog), nil
	}
	if cmd.Name == "ssh" {
		return f.ssh(cmd)
	}
	if cmd.Name == "sbx" {
		blocked := []sbx.NetworkPolicyLog{}
		if f.transfers > 0 {
			blocked = append(blocked, sbx.NetworkPolicyLog{SandboxID: "id", Host: "artifacts.example", Reason: "default-deny", Count: 1, LastSeen: time.Now()})
		}
		data, _ := json.Marshal(map[string]any{"blocked": blocked})
		return string(data), nil
	}
	return "", errors.New("unexpected command")
}
func (f *pluginAttemptCommands) ssh(cmd process.Command) (string, error) {
	if cmd.Stdin != nil {
		f.transfers++
		output := fmt.Sprintf("build log for attempt %d", f.transfers)
		if f.transfers == 1 {
			output += ": artifacts.example"
		}
		return output, errors.New("build failed")
	}
	return "HERDR_SBX_PLUGIN_V1_BEGIN\nLinux aarch64\n" + base64.StdEncoding.EncodeToString([]byte("/home/guest/config.toml")) + "\nabsent\n\n" + base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[]}}`)) + "\nexternal\n\nHERDR_SBX_PLUGIN_V1_END\n", nil
}
