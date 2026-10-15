package guestplugins

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	var files []payloadFile
	var size int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Never export checkout metadata, local state or credentials.
		if entry.Name() == ".git" || entry.Name() == ".env" || strings.HasPrefix(entry.Name(), ".env.") || entry.Name() == ".ssh" || entry.Name() == ".aws" || entry.Name() == ".netrc" || entry.Name() == ".npmrc" || entry.Name() == ".pypirc" || entry.Name() == ".cache" || strings.HasPrefix(entry.Name(), ".herdr-sbx-") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if ignored[filepath.ToSlash(name)] || name == manifestName || name == "herdr-plugin.toml" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			// WalkDir does not follow links, including links to directories.
			return nil
		}
		if strings.ContainsAny(name, "\n\r\\") {
			return errors.New("invalid plugin filename")
		}
		size += info.Size()
		if size > 128<<20 {
			return errors.New("plugin payload exceeds 128 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := int64(0600)
		if info.Mode().Perm()&0111 != 0 {
			mode = 0700
		}
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
		} else if len(data) >= 4 && (bytes.Equal(data[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.Equal(data[:4], []byte{0xfe, 0xed, 0xfa, 0xcf}) || bytes.Equal(data[:4], []byte{0xca, 0xfe, 0xba, 0xbe}) || bytes.HasPrefix(data, []byte("MZ"))) {
			return fmt.Errorf("%s is not a Linux binary", name)
		}
		files = append(files, payloadFile{filepath.ToSlash(name), data, mode})
		return nil
	})
	return files, err
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
	var paths bytes.Buffer
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
	if err != nil {
		return nil, err
	}
	output, err := run(paths.Bytes(), "--git-dir="+dir, "--work-tree="+ignoreRoot, "-c", "core.excludesFile=/dev/null", "check-ignore", "--no-index", "-z", "--stdin")
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, fmt.Errorf("read plugin ignore rules: %w", err)
	}
	ignored := map[string]bool{}
	for _, name := range strings.Split(string(output), "\x00") {
		if name != "" {
			relative, err := filepath.Rel(root, name)
			if err != nil {
				return nil, err
			}
			ignored[filepath.ToSlash(relative)] = true
		}
	}
	return ignored, nil
}

// linuxBuildCommands preserves manifest order and argv boundaries.
func linuxBuildCommands(manifest map[string]any) ([][]string, error) {
	var commands [][]string
	if manifest["build"] == nil {
		return nil, nil
	}
	builds, ok := manifest["build"].([]any)
	if !ok {
		return nil, errors.New("invalid plugin build entries")
	}
	for _, value := range builds {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("invalid plugin build entry")
		}
		platforms := keyNames(entry["platforms"])
		linux := len(platforms) == 0
		for _, platform := range platforms {
			linux = linux || platform == "linux"
		}
		if !linux {
			continue
		}
		raw, ok := entry["command"].([]any)
		if !ok {
			return nil, errors.New("plugin build command must be an argv array")
		}
		argv := make([]string, len(raw))
		for i, value := range raw {
			var ok bool
			argv[i], ok = value.(string)
			if !ok {
				return nil, errors.New("invalid plugin build argument")
			}
		}
		if len(argv) == 0 || argv[0] == "" {
			return nil, errors.New("plugin build command is empty")
		}
		for _, arg := range argv {
			if strings.ContainsRune(arg, 0) {
				return nil, errors.New("invalid plugin build argument")
			}
		}
		commands = append(commands, argv)
	}
	return commands, nil
}
