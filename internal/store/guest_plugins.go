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
// Bundle is an optional directory containing a Linux-ready plugin distribution.
type GuestPluginDefault struct {
	Enabled bool   `json:"enabled"`
	Bundle  string `json:"bundle,omitempty"`
}

// GuestPluginDefaults contains explicit choices only. Missing plugins are off.
type GuestPluginDefaults struct {
	Plugins map[string]GuestPluginDefault `json:"plugins"`
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
		// Preserve an explicit opt-in from the first Reviewr-only implementation.
		legacy, err := ReadStatusStore()
		if err != nil {
			return settings, err
		}
		if legacy.ReviewrEnabled {
			settings.Plugins["persiyanov.reviewr"] = GuestPluginDefault{Enabled: true}
		}
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

// SetGuestPluginDefault changes one choice under its own lock. Bundle is left
// unchanged when nil, so toggling never forgets a prepared distribution path.
func SetGuestPluginDefault(id string, enabled bool, bundle *string) error {
	return updateGuestPluginDefault(id, func(entry *GuestPluginDefault) {
		entry.Enabled = enabled
		if bundle != nil {
			entry.Bundle = *bundle
		}
	})
}

// SetGuestPluginBundle preserves the enabled choice under the settings lock.
func SetGuestPluginBundle(id, path string) error {
	return updateGuestPluginDefault(id, func(entry *GuestPluginDefault) { entry.Bundle = path })
}

func updateGuestPluginDefault(id string, change func(*GuestPluginDefault)) error {
	if !ValidPluginID(id) || id == "lanwen.herdr-sbx" {
		return errors.New("invalid guest plugin ID")
	}
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
	entry := settings.Plugins[id]
	change(&entry)
	settings.Plugins[id] = entry
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
