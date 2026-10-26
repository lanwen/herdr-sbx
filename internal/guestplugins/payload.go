package guestplugins

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// planPayload validates source files before transfer and applies Git ignore rules.
// An empty arch validates the host payload before a guest architecture is known.
func planPayload(root, manifestPath string, manifest map[string]any, arch string) ([]payloadFile, error) {
	if _, err := linuxBuildCommands(manifest); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = resolved
	selected, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !selected.IsDir() {
		return nil, errors.New("plugin source root is not a directory")
	}
	source, err := os.OpenRoot(root + "/.")
	if err != nil {
		return nil, err
	}
	defer source.Close()
	opened, err := source.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(selected, opened) {
		return nil, errors.New("plugin source root changed while opening")
	}
	ignored, err := ignoredSourceFiles(root)
	if err != nil {
		return nil, err
	}
	manifestDir, err := filepath.EvalSymlinks(filepath.Dir(manifestPath))
	if err != nil {
		return nil, err
	}
	manifestName, err := filepath.Rel(root, filepath.Join(manifestDir, filepath.Base(manifestPath)))
	if err != nil {
		return nil, err
	}
	walker := payloadWalker{ignored: ignored, manifestName: manifestName, arch: arch}
	err = walker.walk(source, "")
	return walker.files, err
}

type payloadWalker struct {
	ignored            map[string]bool
	manifestName, arch string
	files              []payloadFile
	size               int64
}

func (w *payloadWalker) walk(dir *os.Root, prefix string) error {
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := w.visit(dir, prefix, entry); err != nil {
			return err
		}
	}
	return nil
}

func privateSourceName(name string) bool {
	return slices.Contains([]string{".git", ".env", ".ssh", ".aws", ".netrc", ".npmrc", ".pypirc", ".cache"}, name) || strings.HasPrefix(name, ".env.") || strings.HasPrefix(name, ".herdr-sbx-")
}

func (w *payloadWalker) visit(dir *os.Root, prefix string, entry fs.DirEntry) error {
	// Never export checkout metadata, local state or credentials.
	if privateSourceName(entry.Name()) {
		return nil
	}
	name := filepath.Join(prefix, entry.Name())
	info, err := dir.Lstat(entry.Name())
	if err != nil {
		return err
	}
	if info.IsDir() {
		child, err := openPayloadDirectory(dir, entry.Name(), info)
		if err != nil {
			return err
		}
		defer child.Close()
		return w.walk(child, name)
	}
	return w.addFile(dir, entry.Name(), name, info)
}

func (w *payloadWalker) addFile(dir *os.Root, entry, name string, info fs.FileInfo) error {
	if w.ignored[filepath.ToSlash(name)] || name == w.manifestName || name == "herdr-plugin.toml" {
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if strings.ContainsAny(name, "\n\r\\") {
		return errors.New("invalid plugin filename")
	}
	data, err := readPayloadFile(dir, entry, info, (128<<20)-w.size)
	if err != nil {
		return err
	}
	w.size += int64(len(data))
	mode := int64(0600)
	if info.Mode().Perm()&0111 != 0 {
		mode = 0700
	}
	if err := validatePayloadBinary(name, data, w.arch); err != nil {
		return err
	}
	w.files = append(w.files, payloadFile{filepath.ToSlash(name), data, mode})
	return nil
}

func validatePayloadBinary(name string, data []byte, arch string) error {
	if bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		binary, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer binary.Close()
		want := elf.EM_X86_64
		if strings.HasPrefix(arch, "aarch64") {
			want = elf.EM_AARCH64
		}
		if arch != "" && binary.Machine != want {
			return fmt.Errorf("%s is built for another architecture", name)
		}
		return nil
	}
	if nonLinuxBinary(data) {
		return fmt.Errorf("%s is not a Linux binary", name)
	}
	return nil
}

func nonLinuxBinary(data []byte) bool {
	return len(data) >= 4 && (bytes.Equal(data[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.Equal(data[:4], []byte{0xfe, 0xed, 0xfa, 0xcf}) || bytes.Equal(data[:4], []byte{0xca, 0xfe, 0xba, 0xbe}) || bytes.HasPrefix(data, []byte("MZ")))
}

// openPayloadDirectory pins a selected directory before enumerating its children.
func openPayloadDirectory(parent *os.Root, name string, selected fs.FileInfo) (*os.Root, error) {
	// Slash-dot requires a directory, so a replacement FIFO cannot block opening.
	child, err := parent.OpenRoot(name + "/.")
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err == nil && (!opened.IsDir() || !os.SameFile(selected, opened)) {
		err = fmt.Errorf("plugin source changed while reading directory %s", name)
	}
	if err != nil {
		child.Close()
		return nil, err
	}
	return child, nil
}

// readPayloadFile rejects replaced entries and confines reads to the opened root.
func readPayloadFile(root *os.Root, name string, selected fs.FileInfo, remaining int64) ([]byte, error) {
	// A regular file can become a FIFO after selection; never wait for a writer.
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(selected, opened) {
		return nil, fmt.Errorf("plugin source changed while reading %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > remaining {
		return nil, errors.New("plugin payload exceeds 128 MiB")
	}
	return data, nil
}

func platformTarget(platform string) (string, error) {
	switch platform {
	case "Linux aarch64", "Linux arm64":
		return "aarch64", nil
	case "Linux x86_64":
		return "x86_64", nil
	}
	return "", fmt.Errorf("unsupported guest platform %q", platform)
}

// ignoredSourceFiles uses a clean Git index to apply nested ignore rules even
// for linked source folders which are not themselves Git checkouts.
func ignoredSourceFiles(root string) (map[string]bool, error) {
	dir, err := os.MkdirTemp("", "herdr-sbx-ignore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := func(input []byte, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		cmd.Stdin = bytes.NewReader(input)
		return cmd.Output()
	}
	if _, err := run(nil, "init", "--bare", "--quiet", dir); err != nil {
		return nil, fmt.Errorf("read plugin ignore rules (host git is required): %w", err)
	}
	ignoreRoot := root
	top := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	if output, err := top.Output(); err == nil {
		ignoreRoot = strings.TrimSpace(string(output))
	}
	paths, err := sourceIgnorePaths(root)
	if err != nil {
		return nil, err
	}
	output, err := run(paths, "--git-dir="+dir, "--work-tree="+ignoreRoot, "-c", "core.excludesFile=/dev/null", "check-ignore", "--no-index", "-z", "--stdin")
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, fmt.Errorf("read plugin ignore rules: %w", err)
	}
	return relativeIgnoredPaths(root, output)
}

func sourceIgnorePaths(root string) ([]byte, error) {
	var paths bytes.Buffer
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		paths.WriteString(filepath.ToSlash(path))
		paths.WriteByte(0)
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	return paths.Bytes(), err
}

func relativeIgnoredPaths(root string, output []byte) (map[string]bool, error) {
	ignored := map[string]bool{}
	for _, name := range strings.Split(string(output), "\x00") {
		if name == "" {
			continue
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return nil, err
		}
		ignored[filepath.ToSlash(relative)] = true
	}
	return ignored, nil
}

// linuxBuildCommands preserves manifest order and argv boundaries.
func linuxBuildCommands(manifest map[string]any) ([][]string, error) {
	if manifest["build"] == nil {
		return nil, nil
	}
	builds, ok := manifest["build"].([]any)
	if !ok {
		return nil, errors.New("invalid plugin build entries")
	}
	var commands [][]string
	for _, value := range builds {
		argv, err := linuxBuildCommand(value)
		if err != nil {
			return nil, err
		}
		if argv != nil {
			commands = append(commands, argv)
		}
	}
	return commands, nil
}

func linuxBuildCommand(value any) ([]string, error) {
	entry, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("invalid plugin build entry")
	}
	platforms := keyNames(entry["platforms"])
	if len(platforms) > 0 && !slices.Contains(platforms, "linux") {
		return nil, nil
	}
	raw, ok := entry["command"].([]any)
	if !ok {
		return nil, errors.New("plugin build command must be an argv array")
	}
	return pluginBuildArguments(raw)
}

func pluginBuildArguments(raw []any) ([]string, error) {
	argv := make([]string, len(raw))
	for i, value := range raw {
		arg, ok := value.(string)
		if !ok {
			return nil, errors.New("invalid plugin build argument")
		}
		argv[i] = arg
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("plugin build command is empty")
	}
	for _, arg := range argv {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("invalid plugin build argument")
		}
	}
	return argv, nil
}
