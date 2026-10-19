package guestplugins

import (
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestCatalogDiscoveryDoesNotOptIn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		return `{"result":{"plugins":[{"plugin_id":"old.plugin","enabled":false},{"plugin_id":"new.plugin","enabled":true},{"plugin_id":"lanwen.herdr-sbx","enabled":true}]}}`, nil
	}})
	if err := store.SetGuestPluginDefault("old.plugin", true); err != nil {
		t.Fatal(err)
	}
	choices, err := Choices(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || choices[0].ID != "new.plugin" || choices[0].Enabled || !choices[1].Enabled {
		t.Fatalf("choices: %+v", choices)
	}
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil || len(settings.Plugins) != 1 {
		t.Fatal("discovery wrote defaults", err)
	}
	if err := store.SetGuestPluginDefault("old.plugin", false); err != nil {
		t.Fatal(err)
	}
	noCommands := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		t.Fatal("opt-out contacted a guest or local Herdr")
		return "", nil
	}})
	if err := ApplySandbox(context.Background(), noCommands, "unused", "fixture-scope", "fixture-sandbox", nil); err != nil {
		t.Fatal(err)
	}
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
				if err := os.WriteFile(manifest, []byte(test.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := unsupportedReason(plugin); !strings.Contains(got, test.want) || (test.want == "" && got != "") {
				t.Fatalf("reason %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnsupportedSelectionSkipsGuest(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := store.SetGuestPluginDefault("example.plugin", true); err != nil {
		t.Fatal(err)
	}
	client := herdr.New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "ssh" {
			t.Fatal("unsupported plugin contacted guest")
		}
		return `{"result":{"plugins":[{"plugin_id":"example.plugin","version":"1.0.0","plugin_root":"/missing/plugin"}]}}`, nil
	}})
	err := ApplySandbox(context.Background(), client, "unused", "fixture-scope", "fixture-sandbox", func() bool { return true })
	if err == nil || !strings.Contains(err.Error(), "unsupported: Cannot read the local plugin manifest") {
		t.Fatalf("missing support reason: %v", err)
	}
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
			if err := os.Symlink("missing", path); err != nil {
				t.Fatal(err)
			}
		}, ""},
		{"special file", func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
		}, ""},
		{"oversized", func(t *testing.T, path string) {
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := f.Truncate(129 << 20); err != nil {
				t.Fatal(err)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			client := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) { return string(catalog), nil }})
			choices, err := Choices(context.Background(), client)
			if err != nil || len(choices) != 1 {
				t.Fatalf("choices=%+v err=%v", choices, err)
			}
			got := choices[0].UnsupportedReason
			if test.want == "" && got != "" || !strings.Contains(got, test.want) {
				t.Fatalf("reason %q, want %q", got, test.want)
			}
		})
	}
}

func writePayloadFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
