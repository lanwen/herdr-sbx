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
	selected := 0
	changed := false
	needsPrefix := false
	for _, value := range commands {
		binding, ok := value.(map[string]any)
		if !ok {
			continue
		}
		action, _ := binding["command"].(string)
		if binding["type"] != "plugin_action" || !strings.HasPrefix(action, pluginID+".") {
			continue
		}
		selected++
		keys := keyNames(binding["key"])
		if len(keys) == 0 {
			return nil, errors.New("plugin shortcut has no valid key")
		}
		found := false
		for _, key := range keys {
			needsPrefix = needsPrefix || strings.Contains(key, "prefix+")
			for _, entry := range existing {
				other, _ := entry.(map[string]any)
				for _, used := range keyNames(other["key"]) {
					if key == used {
						if !reflect.DeepEqual(binding, other) {
							return nil, fmt.Errorf("guest shortcut %s is already assigned; resolve it before enabling plugin", key)
						}
						found = true
					}
				}
			}
			for name, value := range gk {
				if name == "command" || name == "prefix" {
					continue
				}
				for _, used := range keyNames(value) {
					if key == used {
						return nil, fmt.Errorf("guest shortcut %s conflicts with %s", key, name)
					}
				}
			}
		}
		if !found {
			existing = append(existing, binding)
			changed = true
		}
	}
	if selected == 0 {
		return bytes.Clone(guest), nil
	}
	if needsPrefix {
		hostPrefix := keyNames(hk["prefix"])
		if len(hostPrefix) == 0 {
			hostPrefix = []string{"ctrl+b"}
		}
		guestPrefix := keyNames(gk["prefix"])
		if len(guestPrefix) == 0 {
			for _, prefix := range hostPrefix {
				for _, entry := range existing {
					binding, _ := entry.(map[string]any)
					for _, key := range keyNames(binding["key"]) {
						if key == prefix {
							return nil, fmt.Errorf("local prefix %s conflicts with a guest shortcut", prefix)
						}
					}
				}
				for name, value := range gk {
					if name == "command" || name == "prefix" {
						continue
					}
					for _, key := range keyNames(value) {
						if key == prefix {
							return nil, fmt.Errorf("local prefix %s conflicts with guest %s", prefix, name)
						}
					}
				}
			}
			guestPrefix = hostPrefix
			gk["prefix"] = hostPrefix
			changed = true
		}
		// Explicit guest prefixes remain authoritative; do not replace them.
		for _, wanted := range hostPrefix {
			found := false
			for _, have := range guestPrefix {
				found = found || wanted == have
			}
			if !found {
				return nil, fmt.Errorf("guest Herdr prefix does not include %s; configure matching prefixes before syncing plugin", wanted)
			}
		}
	}
	if !changed {
		return bytes.Clone(guest), nil
	}
	gk["command"] = existing
	return toml.Marshal(remote)
}
