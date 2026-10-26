// Package guestplugins prepares selected plugins for guest herdr servers.
// Payload and configuration planning run on the host; herdr owns SSH transport.
package guestplugins

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func localConfig() ([]byte, error) {
	path := os.Getenv("HERDR_CONFIG_PATH")
	if path == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			base = filepath.Join(home, ".config")
		}
		path = filepath.Join(base, "herdr", "config.toml")
	}
	return os.ReadFile(path)
}

func parseConfig(data []byte) (map[string]any, error) {
	result := map[string]any{}
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, errors.New("invalid Herdr TOML configuration")
	}
	return result, nil
}
func keyNames(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, k := range v {
			if s, ok := k.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// mergeKeys preserves all guest settings and rejects collisions. If changes are
// needed TOML is reserialized (comments are retained in the on-guest backup).
func mergeKeys(local, guest []byte, pluginID string) ([]byte, error) {
	host, err := parseConfig(local)
	if err != nil {
		return nil, err
	}
	remote, err := parseConfig(guest)
	if err != nil {
		return nil, err
	}
	hk, _ := host["keys"].(map[string]any)
	gk, _ := remote["keys"].(map[string]any)
	if gk == nil {
		gk = map[string]any{}
		remote["keys"] = gk
	}
	commands, _ := hk["command"].([]any)
	existing, _ := gk["command"].([]any)
	merge := shortcutMerge{keys: gk, commands: existing}
	if err := merge.addBindings(commands, pluginID); err != nil {
		return nil, err
	}
	if merge.selected == 0 {
		return bytes.Clone(guest), nil
	}
	if merge.needsPrefix {
		changed, err := mergePrefix(hk, gk, merge.commands)
		if err != nil {
			return nil, err
		}
		merge.changed = merge.changed || changed
	}
	if !merge.changed {
		return bytes.Clone(guest), nil
	}
	gk["command"] = merge.commands
	return toml.Marshal(remote)
}

type shortcutMerge struct {
	keys                 map[string]any
	commands             []any
	selected             int
	changed, needsPrefix bool
}

func (m *shortcutMerge) addBindings(commands []any, pluginID string) error {
	for _, value := range commands {
		if err := m.add(value, pluginID); err != nil {
			return err
		}
	}
	return nil
}

func (m *shortcutMerge) add(value any, pluginID string) error {
	binding, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	action, _ := binding["command"].(string)
	if binding["type"] != "plugin_action" || !strings.HasPrefix(action, pluginID+".") {
		return nil
	}
	m.selected++
	keys := keyNames(binding["key"])
	if len(keys) == 0 {
		return errors.New("plugin shortcut has no valid key")
	}
	found, err := m.addKeys(binding, keys)
	if err != nil {
		return err
	}
	if !found {
		m.commands = append(m.commands, binding)
		m.changed = true
	}
	return nil
}

func (m *shortcutMerge) addKeys(binding map[string]any, keys []string) (bool, error) {
	found := false
	for _, key := range keys {
		m.needsPrefix = m.needsPrefix || strings.Contains(key, "prefix+")
		matched, err := matchingShortcut(key, binding, m.commands)
		if err != nil {
			return false, err
		}
		found = found || matched
		if name := namedShortcut(key, m.keys); name != "" {
			return false, fmt.Errorf("guest shortcut %s conflicts with %s", key, name)
		}
	}
	return found, nil
}

func matchingShortcut(key string, binding map[string]any, commands []any) (bool, error) {
	found := false
	for _, entry := range commands {
		other, _ := entry.(map[string]any)
		for _, used := range keyNames(other["key"]) {
			if key != used {
				continue
			}
			if !reflect.DeepEqual(binding, other) {
				return false, fmt.Errorf("guest shortcut %s is already assigned; resolve it before enabling plugin", key)
			}
			found = true
		}
	}
	return found, nil
}

func namedShortcut(key string, keys map[string]any) string {
	for name, value := range keys {
		if name == "command" || name == "prefix" {
			continue
		}
		if slices.Contains(keyNames(value), key) {
			return name
		}
	}
	return ""
}

func prefixConflict(prefix string, commands []any, keys map[string]any) error {
	for _, entry := range commands {
		binding, _ := entry.(map[string]any)
		if slices.Contains(keyNames(binding["key"]), prefix) {
			return fmt.Errorf("local prefix %s conflicts with a guest shortcut", prefix)
		}
	}
	if name := namedShortcut(prefix, keys); name != "" {
		return fmt.Errorf("local prefix %s conflicts with guest %s", prefix, name)
	}
	return nil
}

func mergePrefix(host, guest map[string]any, commands []any) (bool, error) {
	hostPrefix := keyNames(host["prefix"])
	if len(hostPrefix) == 0 {
		hostPrefix = []string{"ctrl+b"}
	}
	guestPrefix := keyNames(guest["prefix"])
	changed := len(guestPrefix) == 0
	if changed {
		for _, prefix := range hostPrefix {
			if err := prefixConflict(prefix, commands, guest); err != nil {
				return false, err
			}
		}
		guestPrefix = hostPrefix
		guest["prefix"] = hostPrefix
	}
	// Explicit guest prefixes remain authoritative; do not replace them.
	for _, wanted := range hostPrefix {
		if !slices.Contains(guestPrefix, wanted) {
			return false, fmt.Errorf("guest Herdr prefix does not include %s; configure matching prefixes before syncing plugin", wanted)
		}
	}
	return changed, nil
}
