package guestplugins

import (
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCatalogDiscoveryDoesNotOptIn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		return `{"result":{"plugins":[{"plugin_id":"old.plugin","enabled":false},{"plugin_id":"new.plugin","enabled":true},{"plugin_id":"lanwen.herdr-sbx","enabled":true}]}}`, nil
	}})
	require.NoError(t, store.SetGuestPluginDefault("old.plugin", true))
	choices, err := Choices(context.Background(), client)
	require.NoError(t, err)
	require.Len(t, choices, 2)
	require.EqualValues(t, "new.plugin", choices[0].ID)
	require.False(t, choices[0].Enabled)
	require.True(t, choices[1].Enabled)
	settings, err := store.ReadGuestPluginDefaults()
	require.NoError(t, err)
	require.Len(t, settings.Plugins, 1)
	require.NoError(t, store.SetGuestPluginDefault("old.plugin", false))
	noCommands := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		t.Fatal("opt-out contacted a guest or local Herdr")
		return "", nil
	}})
	require.NoError(t, ApplySandbox(context.Background(), noCommands, SandboxTarget{Address: "unused", Scope: "fixture-scope", SandboxID: "fixture-sandbox"}, ApplyOptions{Current: nil}))
}

func TestUnsupportedReasonExplainsLocalPlugin(t *testing.T) {
	root := t.TempDir()
	plugin := herdr.PluginInstall{ID: "example.plugin", Root: root, ManifestPath: filepath.Join(root, "custom-plugin.toml"), Version: "1.0.0"}
	manifest := plugin.ManifestPath
	for _, test := range []struct {
		name, content, want string
	}{
		{"missing manifest", "", "Cannot read"},
		{"wrong version", "id='example.plugin'\nversion='2.0.0'\n", "does not match"},
		{"no Linux", "id='example.plugin'\nversion='1.0.0'\nplatforms=['darwin']\n", "does not support Linux"},
		{"build required", "id='example.plugin'\nversion='1.0.0'\n[[build]]\ncommand=['make']\n", ""},
		{"portable", "id='example.plugin'\nversion='1.0.0'\nplatforms=['linux']\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.content != "" {
				require.NoError(t, os.WriteFile(manifest, []byte(test.content), 0600))
			}

			got := unsupportedReason(plugin)
			require.Contains(t, got, test.want)
			require.False(t, test.want == "" && got != "")
		})
	}
}

func TestUnsupportedSelectionSkipsGuest(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, store.SetGuestPluginDefault("example.plugin", true))
	client := herdr.New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		require.NotEqual(t, "ssh", command.Name, "unsupported plugin contacted guest")
		return `{"result":{"plugins":[{"plugin_id":"example.plugin","version":"1.0.0","plugin_root":"/missing/plugin"}]}}`, nil
	}})
	err := ApplySandbox(context.Background(), client, SandboxTarget{Address: "unused", Scope: "fixture-scope", SandboxID: "fixture-sandbox"}, ApplyOptions{Current: func() bool { return true }})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported: Cannot read the local plugin manifest")
}

func TestChoicesExplainUnsupportedLocalPayload(t *testing.T) {
	for _, test := range []struct {
		name string
		file func(*testing.T, string)
		want string
	}{
		{"portable", func(t *testing.T, path string) { writePayloadFixture(t, path, []byte("#!/bin/sh\n"), 0700) }, ""},
		{"macOS binary", func(t *testing.T, path string) { writePayloadFixture(t, path, []byte{0xcf, 0xfa, 0xed, 0xfe}, 0700) }, "not a Linux binary"},
		{"Windows binary", func(t *testing.T, path string) { writePayloadFixture(t, path, []byte("MZxx"), 0700) }, "not a Linux binary"},
		{"symlink", func(t *testing.T, path string) {
			require.NoError(t, os.Symlink("missing", path))
		}, ""},
		{"special file", func(t *testing.T, path string) {
			require.NoError(t, syscall.Mkfifo(path, 0600))
		}, ""},
		{"oversized", func(t *testing.T, path string) {
			f, err := os.Create(path)
			require.NoError(t, err)
			defer f.Close()
			require.NoError(t, f.Truncate(129<<20))
		}, "exceeds 128 MiB"},
		{"unreadable", func(t *testing.T, path string) { writePayloadFixture(t, path, []byte("private"), 0000) }, "permission denied"},
		{"guest architecture unknown", func(t *testing.T, path string) {
			data := make([]byte, 64)
			copy(data, []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
			binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_AARCH64))
			binary.LittleEndian.PutUint32(data[20:], uint32(elf.EV_CURRENT))
			binary.LittleEndian.PutUint16(data[52:], 64)
			writePayloadFixture(t, path, data, 0700)
		}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			root := t.TempDir()
			manifest := filepath.Join(root, "custom.toml")
			writePayloadFixture(t, manifest, []byte("id='example.payload'\nversion='1.0.0'\n"), 0600)
			test.file(t, filepath.Join(root, "tool"))
			catalog, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": []herdr.PluginInstall{{ID: "example.payload", Root: root, ManifestPath: manifest, Version: "1.0.0"}}}})
			require.NoError(t, err)
			client := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) { return string(catalog), nil }})
			choices, err := Choices(context.Background(), client)
			require.NoError(t, err)
			require.Len(t, choices, 1)
			got := choices[0].UnsupportedReason
			require.False(t, test.want == "" && got != "")
			require.Contains(t, got, test.want)
		})
	}
}

func writePayloadFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, data, mode))
}
