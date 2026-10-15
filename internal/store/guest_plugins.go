package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// GuestPluginDefault is independent of whether a plugin is enabled locally.
type GuestPluginDefault struct {
	Enabled bool `json:"enabled"`
}

// GuestPluginDefaults contains explicit choices only. Missing plugins are off.
type GuestPluginDefaults struct {
	Plugins   map[string]GuestPluginDefault `json:"plugins"`
	Overrides map[string]map[string]bool    `json:"overrides,omitempty"`
}

var pluginIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func ValidPluginID(id string) bool { return len(id) <= 200 && pluginIDPattern.MatchString(id) }
func GuestPluginDefaultsPath() (string, error) {
	path, err := StatePath()
	return filepath.Join(filepath.Dir(path), "guest-plugins.json"), err
}
func ReadGuestPluginDefaults() (GuestPluginDefaults, error) {
	settings := GuestPluginDefaults{Plugins: map[string]GuestPluginDefault{}}
	path, err := GuestPluginDefaultsPath()
	if err != nil {
		return settings, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return settings, err
	}
	if settings.Plugins == nil {
		settings.Plugins = map[string]GuestPluginDefault{}
	}
	for id := range settings.Plugins {
		if !ValidPluginID(id) {
			return settings, errors.New("invalid plugin ID in guest defaults")
		}
	}
	return settings, nil
}

// SetGuestPluginDefault changes one choice under its own lock.
func SetGuestPluginDefault(id string, enabled bool) error {
	return updateGuestPluginDefault(id, func(entry *GuestPluginDefault) {
		entry.Enabled = enabled
	})
}

func updateGuestPluginDefault(id string, change func(*GuestPluginDefault)) error {
	if !ValidPluginID(id) || id == "lanwen.herdr-sbx" {
		return errors.New("invalid guest plugin ID")
	}
	return updateGuestPluginSettings(func(settings *GuestPluginDefaults) {
		entry := settings.Plugins[id]
		change(&entry)
		settings.Plugins[id] = entry
	})
}

func updateGuestPluginSettings(change func(*GuestPluginDefaults)) error {
	path, err := GuestPluginDefaultsPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	settings, err := ReadGuestPluginDefaults()
	if err != nil {
		return err
	}
	change(&settings)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".guest-plugins-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Effective resolves live defaults and persistent per-sandbox overrides.
func (s GuestPluginDefaults) Effective(scope, id string) GuestPluginDefaults {
	result := GuestPluginDefaults{Plugins: map[string]GuestPluginDefault{}}
	for plugin, entry := range s.Plugins {
		result.Plugins[plugin] = entry
	}
	for plugin, enabled := range s.Overrides[StatusKey(scope, id)] {
		entry := result.Plugins[plugin]
		entry.Enabled = enabled
		result.Plugins[plugin] = entry
	}
	return result
}

// SetSandboxPluginOverride saves On/Off, or clears the override for Inherit.
func SetSandboxPluginOverride(scope, sandboxID, plugin string, enabled *bool) error {
	if scope == "" || sandboxID == "" || !ValidPluginID(plugin) || plugin == "lanwen.herdr-sbx" {
		return errors.New("invalid sandbox plugin selection")
	}
	return updateGuestPluginSettings(func(settings *GuestPluginDefaults) {
		key := StatusKey(scope, sandboxID)
		if enabled == nil {
			delete(settings.Overrides[key], plugin)
			if len(settings.Overrides[key]) == 0 {
				delete(settings.Overrides, key)
			}
			return
		}
		if settings.Overrides == nil {
			settings.Overrides = map[string]map[string]bool{}
		}
		if settings.Overrides[key] == nil {
			settings.Overrides[key] = map[string]bool{}
		}
		settings.Overrides[key][plugin] = *enabled
	})
}
