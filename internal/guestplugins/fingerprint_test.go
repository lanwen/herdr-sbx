package guestplugins

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
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
		if cmd.Name != "herdr" {
			t.Fatal("fingerprint contacted guest")
		}
		data, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{local}}})
		return string(data), err
	}})
	if err := store.SetGuestPluginDefault(local.ID, true); err != nil {
		t.Fatal(err)
	}
	fingerprint := func() string {
		t.Helper()
		value, err := DesiredFingerprint(context.Background(), client, "scope", "sandbox")
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	previous := fingerprint()
	writePayloadFixture(t, filepath.Join(root, "artifact"), []byte("ignored build output"), 0600)
	if got := fingerprint(); got != previous {
		t.Fatal("ignored build artifact invalidated observation")
	}
	writePayloadFixture(t, filepath.Join(root, "source.sh"), []byte("echo changed"), 0700)
	changed := fingerprint()
	if changed == previous {
		t.Fatal("source change did not invalidate observation")
	}
	previous = changed
	writePayloadFixture(t, config, []byte("[shortcuts]\n"), 0600)
	changed = fingerprint()
	if changed == previous {
		t.Fatal("local shortcut configuration change did not invalidate observation")
	}
	previous = changed
	writePayloadFixture(t, manifest, []byte("# changed manifest\nid='example.plugin'\nversion='1.0.0'\n"), 0600)
	changed = fingerprint()
	if changed == previous {
		t.Fatal("exact manifest bytes did not invalidate observation")
	}
	off := false
	if err := store.SetSandboxPluginOverride("scope", "sandbox", local.ID, &off); err != nil {
		t.Fatal(err)
	}
	previous = fingerprint()
	if previous == changed {
		t.Fatal("effective per-sandbox Off did not invalidate observation")
	}
	// An inherited and an explicit Off both leave existing guest files untouched.
	if err := store.SetGuestPluginDefault(local.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSandboxPluginOverride("scope", "sandbox", local.ID, nil); err != nil {
		t.Fatal(err)
	}
	if fingerprint() != previous {
		t.Fatal("equivalent effective settings changed fingerprint")
	}

}
