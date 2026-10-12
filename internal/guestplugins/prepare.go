package guestplugins

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/pelletier/go-toml/v2"
)

// Apply reconciles explicit defaults, independent of the local enabled flags.
// One failed plugin does not prevent the other selected plugins from applying.
func Apply(ctx context.Context, client *herdr.Client, transport http.RoundTripper, target string, current func() bool) error {
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return err
	}
	selected := false
	for _, entry := range settings.Plugins {
		selected = selected || entry.Enabled
	}
	if !selected {
		return nil
	}
	catalog, err := client.LocalPlugins(ctx)
	if err != nil {
		return err
	}
	installed := map[string]herdr.PluginInstall{}
	for _, plugin := range catalog {
		installed[plugin.ID] = plugin
	}
	ids := make([]string, 0, len(settings.Plugins))
	for id := range settings.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		desired := settings.Plugins[id]
		if !desired.Enabled {
			continue
		}
		local, ok := installed[id]
		if !ok && desired.Bundle == "" {
			failures = append(failures, fmt.Errorf("%s: not installed locally", id))
			continue
		}
		if local.ID == "" {
			local.ID = id
		}
		stillSelected := func() bool {
			latest, err := store.ReadGuestPluginDefaults()
			return err == nil && latest.Plugins[id] == desired && current != nil && current()
		}
		if err := prepare(ctx, client, transport, target, local, desired.Bundle, stillSelected); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
		}
	}
	return errors.Join(failures...)
}

type payloadFile struct {
	name string
	data []byte
	mode int64
}

func prepare(ctx context.Context, client *herdr.Client, transport http.RoundTripper, target string, local herdr.PluginInstall, bundleRoot string, current func() bool) error {
	if !store.ValidPluginID(local.ID) || local.ID == "lanwen.herdr-sbx" {
		return errors.New("invalid guest plugin")
	}
	config, err := localConfig()
	if errors.Is(err, os.ErrNotExist) {
		config = nil
	} else if err != nil {
		return errors.New("cannot read local Herdr shortcuts")
	}
	guest, err := client.InspectGuestPlugin(ctx, target, local.ID)
	if err != nil {
		return err
	}
	merged, err := mergeKeys(config, guest.Config, local.ID)
	if err != nil {
		return err
	}
	arch, err := platformTarget(guest.Platform)
	if err != nil {
		return err
	}
	source := local.Root
	if bundleRoot != "" {
		source = bundleRoot
	}
	manifest, err := readSmall(filepath.Join(source, "herdr-plugin.toml"))
	if err != nil {
		return err
	}
	doc, err := parseConfig(manifest)
	if err != nil {
		return errors.New("invalid plugin manifest")
	}
	version, _ := doc["version"].(string)
	if doc["id"] != local.ID || version == "" || (bundleRoot == "" && version != local.Version) {
		return errors.New("plugin manifest does not match installed metadata")
	}
	platforms := keyNames(doc["platforms"])
	if len(platforms) > 0 {
		linux := false
		for _, p := range platforms {
			linux = linux || p == "linux"
		}
		if !linux {
			return errors.New("plugin does not support Linux")
		}
	}
	if guest.Plugin.ID != "" && !guest.Managed {
		return errors.New("guest installation is managed outside herdr-sbx; left unchanged")
	}
	files, err := planPayload(local.ID, source, doc, bundleRoot != "", arch)
	if err != nil {
		return err
	}
	delete(doc, "build")
	manifest, err = toml.Marshal(doc)
	if err != nil {
		return err
	}
	files = append(files, payloadFile{"herdr-plugin.toml", manifest, 0600})
	// Content plus version/architecture identify an immutable guest distribution.
	var identity bytes.Buffer
	identity.WriteString(version + arch)
	for _, f := range files {
		fmt.Fprintf(&identity, "%s:%o:%s\n", f.name, f.mode, digest(f.data))
	}
	revision := digest(identity.Bytes())[:24]
	if guest.Managed && strings.HasSuffix(guest.Plugin.Root, "/"+local.ID+"-"+revision) && guest.Plugin.Version == version && guest.Plugin.Enabled && bytes.Equal(merged, guest.Config) && guest.AppliedConfigSHA == digest(guest.Config) {
		return nil
	}
	if ctx.Err() != nil || current == nil || !current() {
		return errors.New("sandbox identity or plugin selection changed before plugin preparation")
	}
	if bundleRoot == "" {
		switch local.ID {
		case reviewrID:
			if !releaseVersion.MatchString(version) {
				return errors.New("a stable Reviewr release is required")
			}
			binary, err := download(ctx, transport, version, arch)
			if err != nil {
				return err
			}
			files = append(files, payloadFile{"bin/herdr-reviewr", binary, 0700})
		case "herdr-last":
			if !releaseVersion.MatchString(version) {
				return errors.New("a stable herdr-last release is required")
			}
			binary, err := downloadLast(ctx, transport, version, arch)
			if err != nil {
				return err
			}
			files = append(files, payloadFile{"bin/herdr-last", binary, 0700})
		}
	}
	bundle, err := makeBundle(files, merged)
	if err != nil {
		return err
	}
	if ctx.Err() != nil || !current() {
		return errors.New("sandbox identity or plugin selection changed before plugin transfer")
	}
	if err := client.InstallGuestPlugin(ctx, target, local.ID, bytes.NewReader(bundle), digest(bundle), digest(guest.Config), revision, guest.ConfigExists); err != nil {
		return err
	}
	verified, err := client.InspectGuestPlugin(ctx, target, local.ID)
	if err != nil {
		return err
	}
	if verified.Plugin.ID != local.ID || !verified.Plugin.Enabled || verified.Plugin.Version != version || verified.AppliedConfigSHA != digest(merged) {
		return errors.New("guest plugin setup could not be verified")
	}
	return nil
}
func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read plugin files")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("invalid plugin file")
	}
	return io.ReadAll(io.LimitReader(f, 1<<20))
}
func makeBundle(files []payloadFile, config []byte) ([]byte, error) {
	var buffer bytes.Buffer
	tw := tar.NewWriter(&buffer)
	sums := ""
	write := func(name string, data []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: mode}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	for _, f := range files {
		if err := write("plugin/"+f.name, f.data, f.mode); err != nil {
			return nil, err
		}
		sums += fmt.Sprintf("%s  %s\n", digest(f.data), f.name)
	}
	if err := write("files.sha256", []byte(sums), 0600); err != nil {
		return nil, err
	}
	if err := write("config.toml", config, 0600); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
