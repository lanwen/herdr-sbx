package guestplugins

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Built-in recipes only contain public release layout knowledge. Other plugins
// use a self-contained Linux distribution supplied independently of selection.
func planPayload(id, root string, manifest map[string]any, prepared bool, arch string) ([]payloadFile, error) {
	if !prepared {
		switch id {
		case reviewrID:
			script, err := readSmall(filepath.Join(root, "herdr", "pane.sh"))
			return []payloadFile{{"herdr/pane.sh", script, 0700}}, err
		case "herdr-last":
			return nil, nil
		default:
			if builds, ok := manifest["build"].([]any); ok && len(builds) > 0 {
				return nil, errors.New("a Linux-ready bundle is required; configure guest-plugins bundle <id> <directory>")
			}
		}
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = resolved
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
		// Never export checkout metadata, dotfiles, local state or credentials.
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if name == "herdr-plugin.toml" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("bundle contains a symlink or special file: %s", name)
		}
		if strings.ContainsAny(name, "\n\r\\") {
			return errors.New("invalid bundle filename")
		}
		size += info.Size()
		if size > 128<<20 {
			return errors.New("plugin bundle exceeds 128 MiB")
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
			if binary.Machine != want {
				return fmt.Errorf("%s is built for another architecture", name)
			}
		} else if len(data) >= 4 && (bytes.Equal(data[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.Equal(data[:4], []byte{0xfe, 0xed, 0xfa, 0xcf}) || bytes.Equal(data[:4], []byte{0xca, 0xfe, 0xba, 0xbe}) || bytes.HasPrefix(data, []byte("MZ"))) {
			return fmt.Errorf("%s is not a Linux binary; supply a Linux-ready bundle", name)
		}
		files = append(files, payloadFile{filepath.ToSlash(name), data, mode})
		return nil
	})
	return files, err
}
