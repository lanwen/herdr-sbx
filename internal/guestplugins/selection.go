package guestplugins

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Choice combines the local catalog with separately saved guest defaults.
// Catalog discovery never writes settings or enables a newly installed plugin.
type Choice struct {
	ID                 string
	Enabled, Installed bool
	Bundle             string
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
		choices[id] = Choice{ID: id, Enabled: entry.Enabled, Bundle: entry.Bundle}
	}
	for _, plugin := range catalog {
		if plugin.ID == "lanwen.herdr-sbx" || !store.ValidPluginID(plugin.ID) {
			continue
		}
		choice := choices[plugin.ID]
		choice.ID = plugin.ID
		choice.Installed = true
		choices[plugin.ID] = choice
	}
	result := make([]Choice, 0, len(choices))
	for _, choice := range choices {
		result = append(result, choice)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func PrintChoices(ctx context.Context, client *herdr.Client, out io.Writer) error {
	choices, err := Choices(ctx, client)
	if err != nil {
		return err
	}
	path, err := store.GuestPluginDefaultsPath()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Guest plugin defaults:", path)
	for _, choice := range choices {
		state := "OFF"
		if choice.Enabled {
			state = "ON"
		}
		note := ""
		if !choice.Installed {
			note = " (not installed locally)"
		}
		if choice.Bundle != "" {
			note += " bundle=" + choice.Bundle
		}
		fmt.Fprintf(out, "%-3s %s%s\n", state, choice.ID, note)
	}
	return nil
}
