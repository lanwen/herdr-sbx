package guestplugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// DesiredFingerprint identifies the effective selection, transferable source,
// and local shortcuts without contacting the guest. The caller adds sandbox and
// connection identity to its saved observation.
func DesiredFingerprint(ctx context.Context, client *herdr.Client, scope, sandboxID string) (string, error) {
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return "", err
	}
	effective := settings.Effective(scope, sandboxID)
	ids := enabledPluginIDs(effective)
	// Explicit Off and inherited Off have the same effect. No guest operation is due.
	if len(ids) == 0 {
		return digest(nil), nil
	}
	config, err := localConfig()
	if errors.Is(err, os.ErrNotExist) {
		config = nil
	} else if err != nil {
		return "", fmt.Errorf("read local herdr shortcuts: %w", err)
	}
	catalog, err := client.LocalPlugins(ctx)
	if err != nil {
		return "", err
	}
	installed := map[string]herdr.PluginInstall{}
	for _, plugin := range catalog {
		installed[plugin.ID] = plugin
	}
	sources := []fingerprintSource{}
	for _, id := range ids {
		plugin, found := installed[id]
		if !found {
			sources = append(sources, fingerprintSource{Plugin: herdr.PluginInstall{ID: id}, Unavailable: "not installed"})
			continue
		}
		sources = append(sources, snapshotFingerprintSource(plugin))
	}
	data, err := json.Marshal(struct {
		Sources []fingerprintSource
		Config  []byte
	}{sources, config})
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

type fingerprintSource struct {
	Plugin      herdr.PluginInstall
	Manifest    []byte
	Files       []string
	Unavailable string
}

func snapshotFingerprintSource(plugin herdr.PluginInstall) fingerprintSource {
	snapshot := fingerprintSource{Plugin: plugin}
	manifest, err := readSmall(plugin.ManifestPath)
	if err != nil {
		snapshot.Unavailable = err.Error()
		return snapshot
	}
	snapshot.Manifest = manifest
	doc, err := parseConfig(manifest)
	if err != nil {
		snapshot.Unavailable = err.Error()
		return snapshot
	}
	files, err := planPayload(plugin.Root, plugin.ManifestPath, doc, "")
	if err != nil {
		snapshot.Unavailable = err.Error()
		return snapshot
	}
	for _, file := range files {
		snapshot.Files = append(snapshot.Files, fmt.Sprintf("%s:%o:%s", file.name, file.mode, digest(file.data)))
	}
	return snapshot
}

func enabledPluginIDs(settings store.GuestPluginDefaults) []string {
	ids := []string{}
	for id, choice := range settings.Plugins {
		if choice.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
