package guestplugins

import (
	"context"
	"sort"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Choice combines the local catalog with separately saved guest defaults.
// Catalog discovery never writes settings or enables a newly installed plugin.
type Choice struct {
	ID                 string
	Enabled, Installed bool
	UnsupportedReason  string
}

// unsupportedReason checks the installed plugin without building or copying it.
// Guest architecture is checked later during sync.
func unsupportedReason(plugin herdr.PluginInstall) string {
	if plugin.Root == "" {
		return "Local plugin files are unavailable."
	}
	manifest, err := readSmall(plugin.ManifestPath)
	if err != nil {
		return "Cannot read the local plugin manifest."
	}
	doc, err := parseConfig(manifest)
	if err != nil {
		return "The local plugin manifest is invalid."
	}
	version, _ := doc["version"].(string)
	if doc["id"] != plugin.ID || version == "" || version != plugin.Version {
		return "The local plugin manifest does not match the installed plugin."
	}
	platforms := keyNames(doc["platforms"])
	if len(platforms) > 0 {
		linux := false
		for _, platform := range platforms {
			linux = linux || platform == "linux"
		}
		if !linux {
			return "The plugin does not support Linux sandboxes."
		}
	}
	if _, err := planPayload(plugin.Root, plugin.ManifestPath, doc, ""); err != nil {
		return "The local plugin files cannot be synchronized: " + err.Error()
	}
	return ""
}

func Choices(ctx context.Context, client *herdr.Client) ([]Choice, error) {
	catalog, err := client.LocalPlugins(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return nil, err
	}
	choices := map[string]Choice{}
	for id, entry := range settings.Plugins {
		choices[id] = Choice{ID: id, Enabled: entry.Enabled, UnsupportedReason: "Plugin is not installed locally."}
	}
	for _, plugin := range catalog {
		if plugin.ID == "lanwen.herdr-sbx" || !store.ValidPluginID(plugin.ID) {
			continue
		}
		choice := choices[plugin.ID]
		choice.ID = plugin.ID
		choice.Installed = true
		choice.UnsupportedReason = unsupportedReason(plugin)
		choices[plugin.ID] = choice
	}
	result := make([]Choice, 0, len(choices))
	for _, choice := range choices {
		result = append(result, choice)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
