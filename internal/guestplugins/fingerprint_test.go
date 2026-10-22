package guestplugins

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDesiredFingerprintTracksTransferableSourceAndEffectiveConfiguration(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	root := t.TempDir()
	config := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("HERDR_CONFIG_PATH", config)
	manifest := filepath.Join(root, "source.toml")
	writePayloadFixture(t, manifest, []byte("id='example.plugin'\nversion='1.0.0'\n"), 0600)
	writePayloadFixture(t, filepath.Join(root, ".gitignore"), []byte("artifact\n"), 0600)
	writePayloadFixture(t, filepath.Join(root, "source.sh"), []byte("echo original"), 0700)
	local := herdr.PluginInstall{ID: "example.plugin", Root: root, ManifestPath: manifest, Version: "1.0.0"}
	client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		require.Equal(t, "herdr", cmd.Name, "fingerprint contacted guest")
		data, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{local}}})
		return string(data), err
	}})
	require.NoError(t, store.SetGuestPluginDefault(local.ID, true))
	fingerprint := func() string {
		t.Helper()
		value, err := DesiredFingerprint(context.Background(), client, "scope", "sandbox")
		require.NoError(t, err)
		return value
	}
	previous := fingerprint()
	writePayloadFixture(t, filepath.Join(root, "artifact"), []byte("ignored build output"), 0600)

	got := fingerprint()
	require.Equal(t, previous, got, "ignored build artifact invalidated observation")

	writePayloadFixture(t, filepath.Join(root, "source.sh"), []byte("echo changed"), 0700)
	changed := fingerprint()
	require.NotEqual(t, previous, changed, "source change did not invalidate observation")
	previous = changed
	writePayloadFixture(t, config, []byte("[shortcuts]\n"), 0600)
	changed = fingerprint()
	require.NotEqual(t, previous, changed, "local shortcut configuration change did not invalidate observation")
	previous = changed
	writePayloadFixture(t, manifest, []byte("# changed manifest\nid='example.plugin'\nversion='1.0.0'\n"), 0600)
	changed = fingerprint()
	require.NotEqual(t, previous, changed, "exact manifest bytes did not invalidate observation")
	off := false
	require.NoError(t, store.SetSandboxPluginOverride("scope", "sandbox", local.ID, &off))
	previous = fingerprint()
	require.NotEqual(t, changed, previous, "effective per-sandbox Off did not invalidate observation")
	// An inherited and an explicit Off both leave existing guest files untouched.
	require.NoError(t, store.SetGuestPluginDefault(local.ID, false))
	require.NoError(t, store.SetSandboxPluginOverride("scope", "sandbox", local.ID, nil))
	require.Equal(t, previous, fingerprint(), "equivalent effective settings changed fingerprint")
}
