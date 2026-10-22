package guestplugins

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortableBundleAndRejectedPayloads(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\necho ready\n"), 0700)
	os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE=value"), 0600)
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.EqualValues(t, "run.sh", files[0].name)
	require.EqualValues(t, 0700, files[0].mode)
	os.Symlink("run.sh", filepath.Join(root, "link"))
	{
		files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.EqualValues(t, "run.sh", files[0].name)
	}
	os.Remove(filepath.Join(root, "link"))
	os.WriteFile(filepath.Join(root, "mac-binary"), []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0700)
	{
		_, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
		require.Error(t, err, "accepted macOS binary")
	}
}
func TestLinuxBuildCommandsPreserveOrderAndArguments(t *testing.T) {
	commands, err := linuxBuildCommands(map[string]any{"build": []any{
		map[string]any{"platforms": []any{"darwin"}, "command": []any{"mac-build"}},
		map[string]any{"command": []any{"sh", "-c", "echo 'safe argv'"}},
		map[string]any{"platforms": []any{"linux"}, "command": []any{"make", "install"}},
	}})
	require.NoError(t, err)
	require.Len(t, commands, 2)
	require.EqualValues(t, "echo 'safe argv'", commands[0][2])
	require.EqualValues(t, "make", commands[1][0])
}

func TestPayloadMatchesGuestArchitecture(t *testing.T) {
	root := t.TempDir()
	data := make([]byte, 64)
	copy(data, []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_AARCH64))
	binary.LittleEndian.PutUint32(data[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint16(data[52:], 64)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool"), data, 0700))
	{
		_, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
		require.NoError(t, err, "rejected matching Linux binary: %v", err)
	}
	{
		_, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "x86_64")
		require.Error(t, err)
		require.Contains(t, err.Error(), "another architecture")
	}
}

func TestGuestPlatform(t *testing.T) {
	for _, platform := range []string{"Linux aarch64", "Linux arm64", "Linux x86_64"} {
		{
			_, err := platformTarget(platform)
			assert.NoError(t, err, "%s: %v", platform, err)
		}
	}
	for _, platform := range []string{"Darwin arm64", "Linux riscv64"} {
		{
			_, err := platformTarget(platform)
			assert.Error(t, err, "accepted unsupported guest platform %q", platform)
		}
	}
}

func TestPayloadExcludesCatalogManifest(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "custom-manifest.toml")
	require.NoError(t, os.WriteFile(manifest, []byte("id='example.custom'"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0700))
	files, err := planPayload(root, manifest, map[string]any{}, "aarch64")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.EqualValues(t, "run.sh", files[0].name)
}

func TestPayloadUsesNestedGitignoreAndKeepsUsefulDotfiles(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		".gitignore": "/build/\n*.o\n!keep.o\n", "nested/.gitignore": "private/\n",
		"build/tool": "ignored binary", "nested/private/key": "ignored", "nested/drop.o": "ignored", "nested/keep.o": "source", ".cargo/config.toml": "[build]\n", ".env": "SECRET=private", ".git/config": "private checkout", "main.go": "package main",
	} {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	}
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	require.NoError(t, err)
	got := map[string]bool{}
	for _, f := range files {
		got[f.name] = true
	}
	for _, name := range []string{".cargo/config.toml", "nested/keep.o", "main.go"} {
		assert.True(t, got[name], "missing source %s: %v", name, got)
	}
	for _, name := range []string{"build/tool", "nested/private/key", "nested/drop.o", ".env", ".git/config"} {
		assert.False(t, got[name], "included excluded source %s", name)
	}
}

func TestPayloadHonorsParentRepositoryGitignore(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", repo)
	{
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "init: %s %v", output, err)
	}
	root := filepath.Join(repo, "plugins", "example")
	os.MkdirAll(root, 0700)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("plugins/**/build/\n*.o\n"), 0600)
	os.Mkdir(filepath.Join(root, "build"), 0700)
	os.WriteFile(filepath.Join(root, "build", "artifact"), []byte("ignored"), 0600)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("source"), 0600)
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.EqualValues(t, "main.go", files[0].name)
}

func TestPayloadSkipsExternalLinksAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0700))
	// A target that would fail payload validation if read or traversed.
	target := filepath.Join(outside, "host-binary")
	require.NoError(t, os.WriteFile(target, []byte{0xcf, 0xfa, 0xed, 0xfe}, 0000))
	for name, target := range map[string]string{"CLAUDE.md": target, "external-dir": outside, "dangling": "/nonexistent/herdr-sbx-fixture"} {
		require.NoError(t, os.Symlink(target, filepath.Join(root, name)))
	}
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "pipe"), 0600))
	files, err := planPayload(root, filepath.Join(root, "herdr-plugin.toml"), map[string]any{}, "aarch64")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.EqualValues(t, "run.sh", files[0].name)
	require.EqualValues(t, "#!/bin/sh\n", string(files[0].data))
}

func TestPayloadReadConfinesPathsAndChecksIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	require.NoError(t, os.WriteFile(path, []byte("source"), 0600))
	selected, err := os.Stat(path)
	require.NoError(t, err)
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()
	{
		data, err := readPayloadFile(root, "source", selected, 6)
		require.NoError(t, err)
		require.EqualValues(t, "source", string(data))
	}
	require.NoError(t, os.Rename(path, filepath.Join(dir, "original")))
	require.NoError(t, os.WriteFile(path, []byte("replacement"), 0600))
	{
		data, err := readPayloadFile(root, "source", selected, 64)
		require.Error(t, err)
		require.Nil(t, data)
	}
	require.NoError(t, os.Remove(path))
	require.NoError(t, syscall.Mkfifo(path, 0600))
	{
		data, err := readPayloadFile(root, "source", selected, 64)
		require.Error(t, err)
		require.Nil(t, data)
	}
	outside := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, os.WriteFile(outside, []byte("outside fixture"), 0600))
	outsideInfo, err := os.Stat(outside)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "external")))
	{
		data, err := readPayloadFile(root, "external", outsideInfo, 64)
		require.Error(t, err)
		require.Nil(t, data)
	}
}

func TestPayloadReadLimitsActualBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0600))
	selected, err := os.Stat(path)
	require.NoError(t, err)
	// Keep the inode but grow its contents beyond the selected size.
	require.NoError(t, os.WriteFile(path, []byte("grown"), 0600))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()
	{
		data, err := readPayloadFile(root, "source", selected, selected.Size())
		require.Error(t, err)
		require.Nil(t, data)
		require.Contains(t, err.Error(), "128 MiB")
	}
	{
		data, err := readPayloadFile(root, "source", selected, 5)
		require.NoError(t, err)
		require.EqualValues(t, "grown", string(data))
	}
}

func TestPayloadDirectoryRejectsReplacementAndPinsSelectedChildren(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	require.NoError(t, os.Mkdir(path, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "file"), []byte("selected fixture"), 0600))
	selected, err := os.Lstat(path)
	require.NoError(t, err)
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()
	child, err := openPayloadDirectory(root, "source", selected)
	require.NoError(t, err)
	defer child.Close()
	require.NoError(t, os.Rename(path, filepath.Join(dir, "original")))
	other := filepath.Join(dir, ".cache")
	require.NoError(t, os.Mkdir(other, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(other, "file"), []byte("other fixture"), 0600))
	require.NoError(t, os.Symlink(".cache", path))
	if replacement, err := openPayloadDirectory(root, "source", selected); err == nil {
		replacement.Close()
		t.Fatal("accepted directory alias to a different selected entry")
	}
	info, err := child.Lstat("file")
	require.NoError(t, err)
	{
		data, err := readPayloadFile(child, "file", info, 64)
		require.NoError(t, err)
		require.EqualValues(t, "selected fixture", string(data))
	}
	require.NoError(t, os.Remove(path))
	require.NoError(t, syscall.Mkfifo(path, 0600))
	if replacement, err := openPayloadDirectory(root, "source", selected); err == nil {
		replacement.Close()
		t.Fatal("accepted FIFO as a directory")
	}
}
