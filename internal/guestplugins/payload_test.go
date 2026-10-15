package guestplugins

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPortableBundleAndRejectedPayloads(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\necho ready\n"), 0700)
	os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE=value"), 0600)
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	if err != nil || len(files) != 1 || files[0].name != "run.sh" || files[0].mode != 0700 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	os.Symlink("run.sh", filepath.Join(root, "link"))
	if files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64"); err != nil || len(files) != 1 || files[0].name != "run.sh" {
		t.Fatalf("symlink affected regular payload: %+v %v", files, err)
	}
	os.Remove(filepath.Join(root, "link"))
	os.WriteFile(filepath.Join(root, "mac-binary"), []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0700)
	if _, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64"); err == nil {
		t.Fatal("accepted macOS binary")
	}
}
func TestLinuxBuildCommandsPreserveOrderAndArguments(t *testing.T) {
	commands, err := linuxBuildCommands(map[string]any{"build": []any{
		map[string]any{"platforms": []any{"darwin"}, "command": []any{"mac-build"}},
		map[string]any{"command": []any{"sh", "-c", "echo 'safe argv'"}},
		map[string]any{"platforms": []any{"linux"}, "command": []any{"make", "install"}},
	}})
	if err != nil || len(commands) != 2 || commands[0][2] != "echo 'safe argv'" || commands[1][0] != "make" {
		t.Fatalf("commands=%q err=%v", commands, err)
	}
}

func TestPayloadMatchesGuestArchitecture(t *testing.T) {
	root := t.TempDir()
	data := make([]byte, 64)
	copy(data, []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_AARCH64))
	binary.LittleEndian.PutUint32(data[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint16(data[52:], 64)
	if err := os.WriteFile(filepath.Join(root, "tool"), data, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64"); err != nil {
		t.Fatalf("rejected matching Linux binary: %v", err)
	}
	if _, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "x86_64"); err == nil || !strings.Contains(err.Error(), "another architecture") {
		t.Fatalf("wrong architecture was not explained: %v", err)
	}
}

func TestGuestPlatform(t *testing.T) {
	for _, platform := range []string{"Linux aarch64", "Linux arm64", "Linux x86_64"} {
		if _, err := platformTarget(platform); err != nil {
			t.Errorf("%s: %v", platform, err)
		}
	}
	for _, platform := range []string{"Darwin arm64", "Linux riscv64"} {
		if _, err := platformTarget(platform); err == nil {
			t.Errorf("accepted unsupported guest platform %q", platform)
		}
	}
}

func TestPayloadExcludesCatalogManifest(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "custom-manifest.toml")
	if err := os.WriteFile(manifest, []byte("id='example.custom'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	files, err := planPayload(root, manifest, map[string]any{}, "aarch64")
	if err != nil || len(files) != 1 || files[0].name != "run.sh" {
		t.Fatalf("source manifest included in payload: %+v, %v", files, err)
	}
}

func TestPayloadUsesNestedGitignoreAndKeepsUsefulDotfiles(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		".gitignore": "/build/\n*.o\n!keep.o\n", "nested/.gitignore": "private/\n",
		"build/tool": "ignored binary", "nested/private/key": "ignored", "nested/drop.o": "ignored", "nested/keep.o": "source", ".cargo/config.toml": "[build]\n", ".env": "SECRET=private", ".git/config": "private checkout", "main.go": "package main",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.name] = true
	}
	for _, name := range []string{".cargo/config.toml", "nested/keep.o", "main.go"} {
		if !got[name] {
			t.Errorf("missing source %s: %v", name, got)
		}
	}
	for _, name := range []string{"build/tool", "nested/private/key", "nested/drop.o", ".env", ".git/config"} {
		if got[name] {
			t.Errorf("included excluded source %s", name)
		}
	}
}

func TestPayloadHonorsParentRepositoryGitignore(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", repo)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", output, err)
	}
	root := filepath.Join(repo, "plugins", "example")
	os.MkdirAll(root, 0700)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("plugins/**/build/\n*.o\n"), 0600)
	os.Mkdir(filepath.Join(root, "build"), 0700)
	os.WriteFile(filepath.Join(root, "build", "artifact"), []byte("ignored"), 0600)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("source"), 0600)
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	if err != nil || len(files) != 1 || files[0].name != "main.go" {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

func TestPayloadSkipsExternalLinksAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// A target that would fail payload validation if read or traversed.
	target := filepath.Join(outside, "host-binary")
	if err := os.WriteFile(target, []byte{0xcf, 0xfa, 0xed, 0xfe}, 0000); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"CLAUDE.md": target, "external-dir": outside, "dangling": "/nonexistent/herdr-sbx-fixture"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	if err != nil || len(files) != 1 || files[0].name != "run.sh" || string(files[0].data) != "#!/bin/sh\n" {
		t.Fatalf("payload=%+v err=%v", files, err)
	}
}
