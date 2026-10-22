package guestplugins

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// ApplySandbox resolves live defaults for the selected sandbox before each transfer.
// One failed plugin does not prevent the other selected plugins from applying.
func ApplySandbox(ctx context.Context, client *herdr.Client, target, scope, sandboxID string, current func() bool, attempts ...func(id, revision string) (finish func(error) error, err error)) error {
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return err
	}
	settings = settings.Effective(scope, sandboxID)
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
	var failures Failures
	for _, id := range ids {
		desired := settings.Plugins[id]
		if !desired.Enabled {
			continue
		}
		local, ok := installed[id]
		if !ok {
			failures = append(failures, store.PluginFailure{PluginID: id, Code: store.CodeGuestPluginSync, Detail: "Plugin is not installed locally"})
			continue
		}
		if reason := unsupportedReason(local); reason != "" {
			failures = append(failures, store.PluginFailure{PluginID: id, Code: store.CodeGuestPluginSync, Detail: "unsupported: " + reason})
			continue
		}
		stillSelected := func() bool {
			latest, err := store.ReadGuestPluginDefaults()
			return err == nil && latest.Effective(scope, sandboxID).Plugins[id] == desired && current != nil && current()
		}
		if err := prepare(ctx, client, target, scope, sandboxID, local, stillSelected, attempts...); err != nil {
			failures = append(failures, FailureFor(local, "", err))
		}
	}
	if len(failures) > 0 {
		return failures
	}
	return nil
}

type payloadFile struct {
	name string
	data []byte
	mode int64
}

func prepare(ctx context.Context, client *herdr.Client, target, scope, sandboxID string, local herdr.PluginInstall, current func() bool, attempts ...func(id, revision string) (finish func(error) error, err error)) (result error) {
	var revision string
	absent := false
	failureFor := func(err error) store.PluginFailure {
		failure := FailureFor(local, revision, err)
		var setup *herdr.GuestPluginSetupError
		if absent && errors.As(err, &setup) && setup.BuildFailed {
			failure.Recovery, failure.Command = manualRecovery(local, revision, true)
		} else if !absent && failure.Command != "" {
			failure.Recovery, failure.Command = manualRecovery(local, revision, false)
		}
		return failure
	}
	defer func() {
		if result != nil {
			result = &FailureError{Failure: failureFor(result), Cause: result}
		}
	}()
	if !store.ValidPluginID(local.ID) || local.ID == "lanwen.herdr-sbx" {
		return errors.New("invalid guest plugin")
	}
	config, err := localConfig()
	if errors.Is(err, os.ErrNotExist) {
		config = nil
	} else if err != nil {
		return errors.New("cannot read local herdr shortcuts")
	}
	checkCurrent := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == nil || !current() {
			return context.Canceled
		}
		return nil
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	guest, err := client.InspectGuestPlugin(ctx, target, local.ID)
	if err != nil {
		return err
	}
	absent = guest.Plugin.ID == ""
	if exactExternalPlugin(local, guest.Plugin) && cleanInstalledSource(local) && !guest.Managed {
		return nil
	}
	merged, err := mergeKeys(config, guest.Config, local.ID)
	if err != nil {
		return err
	}
	arch, err := platformTarget(guest.Platform)
	if err != nil {
		return err
	}
	manifest, err := readSmall(local.ManifestPath)
	if err != nil {
		return err
	}
	doc, err := parseConfig(manifest)
	if err != nil {
		return errors.New("invalid plugin manifest")
	}
	version, _ := doc["version"].(string)
	if doc["id"] != local.ID || version == "" || version != local.Version {
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
	files, err := planPayload(local.Root, local.ManifestPath, doc, arch)
	if err != nil {
		return err
	}
	commands, err := linuxBuildCommands(doc)
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
	revision = digest(identity.Bytes())[:24]
	repaired := guest.Managed && strings.HasSuffix(guest.Plugin.Root, "/"+local.ID+"-"+revision) && guest.Plugin.Version == version && guest.Plugin.Enabled && guest.Plugin.Available && noPluginWarnings(guest.Plugin)
	if repaired && bytes.Equal(merged, guest.Config) && guest.AppliedConfigSHA == digest(guest.Config) {
		return store.FinishGuestPluginAttempt(scope, sandboxID, local.ID, revision, "")
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	if repaired {
		commands = nil
		attempts = nil
	}
	for _, attempt := range attempts {
		if attempt != nil {
			finish, err := attempt(local.ID, revision)
			if err != nil {
				return err
			}
			if finish != nil {
				defer func() {
					if result != nil {
						result = &FailureError{Failure: failureFor(result), Cause: result}
					}
					finished := finish(result)
					var structured *FailureError
					if errors.As(finished, &structured) {
						result = finished
					} else {
						result = errors.Join(result, finished)
					}
				}()
			}
		}
	}
	bundle, err := makeBundle(files, merged)
	if err != nil {
		return err
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	if err := client.InstallGuestPlugin(ctx, target, local.ID, bytes.NewReader(bundle), digest(bundle), digest(guest.Config), revision, guest.ConfigExists, commands...); err != nil {
		return err
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	verified, err := client.InspectGuestPlugin(ctx, target, local.ID)
	if err != nil {
		return err
	}
	if verified.Plugin.ID != local.ID || !verified.Plugin.Enabled || verified.Plugin.Version != version || !verified.Plugin.Available || !noPluginWarnings(verified.Plugin) || verified.AppliedConfigSHA != digest(merged) {
		return errors.New("guest plugin setup could not be verified")
	}
	return store.FinishGuestPluginAttempt(scope, sandboxID, local.ID, revision, "")
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

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// exactExternalPlugin accepts an externally managed installation only when herdr
// reports the same immutable source revision. Version alone is not sufficient.
func exactExternalPlugin(local, guest herdr.PluginInstall) bool {
	a, b := local.Source, guest.Source
	return local.ID == guest.ID && local.Version == guest.Version && guest.Enabled && guest.Available && noPluginWarnings(guest) && a.Kind == "github" && b.Kind == "github" && regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(a.ResolvedCommit) && a.ResolvedCommit == b.ResolvedCommit && a.Owner == b.Owner && a.Repo == b.Repo && a.Subdir == b.Subdir
}

// cleanInstalledSource prevents an immutable GitHub ref from standing in for
// different local source when the installed checkout was edited.
func cleanInstalledSource(plugin herdr.PluginInstall) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", plugin.Root}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	head, err := run("rev-parse", "HEAD")
	if err != nil || head != plugin.Source.ResolvedCommit {
		return false
	}
	status, err := run("status", "--porcelain", "--untracked-files=normal", "--", ".")
	return err == nil && status == ""
}

func manualRecovery(plugin herdr.PluginInstall, revision string, install bool) (string, string) {
	source := plugin.Source
	if install && source.Kind == "github" && regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(source.ResolvedCommit) && source.Owner != "" && source.Repo != "" && cleanInstalledSource(plugin) {
		repo := source.Owner + "/" + source.Repo
		if source.Subdir != "" {
			repo += "/" + source.Subdir
		}
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
		return "Install this exact revision inside the sandbox, then choose Re-sync.", "herdr plugin install " + quote(repo) + " --ref " + quote(source.ResolvedCommit) + " --yes"
	}
	return fmt.Sprintf("Repair herdr plugin %s inside the sandbox (version %s) at ~/.local/share/herdr-sbx/plugins/%s, then choose Re-sync.", plugin.ID, plugin.Version, plugin.ID+"-"+revision), ""
}

func noPluginWarnings(plugin herdr.PluginInstall) bool {
	warnings := strings.TrimSpace(string(plugin.Warnings))
	return warnings == "" || warnings == "null" || warnings == "[]"
}
