package guestplugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortableBundleAndRejectedPayloads(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\necho ready\n"), 0700)
	os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE=value"), 0600)
	files, err := planPayload("example.plugin", root, map[string]any{}, false, "aarch64-unknown-linux-musl")
	if err != nil || len(files) != 1 || files[0].name != "run.sh" || files[0].mode != 0700 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	os.Symlink("run.sh", filepath.Join(root, "link"))
	if _, err := planPayload("example.plugin", root, map[string]any{}, true, "aarch64-unknown-linux-musl"); err == nil {
		t.Fatal("accepted symlink")
	}
	os.Remove(filepath.Join(root, "link"))
	os.WriteFile(filepath.Join(root, "mac-binary"), []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0700)
	if _, err := planPayload("example.plugin", root, map[string]any{}, true, "aarch64-unknown-linux-musl"); err == nil {
		t.Fatal("accepted macOS binary")
	}
}
func TestBuildPluginRequiresExplicitBundle(t *testing.T) {
	if _, err := planPayload("example.plugin", t.TempDir(), map[string]any{"build": []any{map[string]any{"command": []any{"make"}}}}, false, "aarch64-unknown-linux-musl"); err == nil {
		t.Fatal("accepted unprepared build plugin")
	}
}
