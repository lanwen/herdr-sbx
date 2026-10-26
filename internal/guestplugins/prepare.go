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
	"slices"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// SandboxTarget identifies the guest connection and its durable selection.
type SandboxTarget struct {
	Address, Scope, SandboxID string
}

// BeginAttempt records an installation attempt and returns its completion hook.
type BeginAttempt func(id, revision string) (finish func(error) error, err error)

// ApplyOptions binds guest work to its current selection and attempt journal.
type ApplyOptions struct {
	Current  func() bool
	Attempts []BeginAttempt
}

// ApplySandbox resolves live defaults for the selected sandbox before each transfer.
// One failed plugin does not prevent the other selected plugins from applying.
func ApplySandbox(ctx context.Context, client *herdr.Client, target SandboxTarget, options ApplyOptions) error {
	scope, sandboxID := target.Scope, target.SandboxID
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil {
		return err
	}
	settings = settings.Effective(scope, sandboxID)
	ids := enabledPluginIDs(settings)
	if len(ids) == 0 {
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
	var failures Failures
	for _, id := range ids {
		desired := settings.Plugins[id]
		local, found := installed[id]
		if failure := applySelectedPlugin(ctx, client, target, selectedPlugin{local: local, id: id, desired: desired, installed: found}, options); failure != nil {
			failures = append(failures, *failure)
		}
	}
	if len(failures) > 0 {
		return failures
	}
	return nil
}

type selectedPlugin struct {
	local     herdr.PluginInstall
	id        string
	desired   store.GuestPluginDefault
	installed bool
}

func applySelectedPlugin(ctx context.Context, client *herdr.Client, target SandboxTarget, selected selectedPlugin, options ApplyOptions) *store.PluginFailure {
	local, id := selected.local, selected.id
	if !selected.installed {
		return &store.PluginFailure{PluginID: id, Code: store.CodeGuestPluginSync, Detail: "Plugin is not installed locally"}
	}
	if reason := unsupportedReason(local); reason != "" {
		return &store.PluginFailure{PluginID: id, Code: store.CodeGuestPluginSync, Detail: "unsupported: " + reason}
	}
	stillSelected := func() bool {
		latest, err := store.ReadGuestPluginDefaults()
		return err == nil && latest.Effective(target.Scope, target.SandboxID).Plugins[id] == selected.desired && options.Current != nil && options.Current()
	}
	if err := prepare(ctx, client, target, local, ApplyOptions{Current: stillSelected, Attempts: options.Attempts}); err != nil {
		failure := FailureFor(local, "", err)
		return &failure
	}
	return nil
}

type payloadFile struct {
	name string
	data []byte
	mode int64
}

type pluginPreparation struct {
	ctx                      context.Context
	client                   *herdr.Client
	target, scope, sandboxID string
	local                    herdr.PluginInstall
	current                  func() bool
	absent                   bool
	revision                 string
}

type pluginPlan struct {
	files             []payloadFile
	commands          [][]string
	config            []byte
	version, revision string
}

func prepare(ctx context.Context, client *herdr.Client, target SandboxTarget, local herdr.PluginInstall, options ApplyOptions) (result error) {
	p := pluginPreparation{ctx: ctx, client: client, target: target.Address, scope: target.Scope, sandboxID: target.SandboxID, local: local, current: options.Current}
	defer func() { result = p.failure(result) }()
	if !store.ValidPluginID(local.ID) || local.ID == "lanwen.herdr-sbx" {
		return errors.New("invalid guest plugin")
	}
	config, err := localConfig()
	if errors.Is(err, os.ErrNotExist) {
		config = nil
	} else if err != nil {
		return errors.New("cannot read local herdr shortcuts")
	}
	if err := p.checkCurrent(); err != nil {
		return err
	}
	guest, err := client.InspectGuestPlugin(ctx, target.Address, local.ID)
	if err != nil {
		return err
	}
	p.absent = guest.Plugin.ID == ""
	if exactExternalPlugin(local, guest.Plugin) && cleanInstalledSource(local) && !guest.Managed {
		return nil
	}
	plan, err := p.plan(config, guest)
	if err != nil {
		return err
	}
	p.revision = plan.revision
	return p.apply(guest, plan, options.Attempts)
}

func (p *pluginPreparation) failure(err error) error {
	if err == nil {
		return nil
	}
	failure := FailureFor(p.local, p.revision, err)
	var setup *herdr.GuestPluginSetupError
	if p.absent && errors.As(err, &setup) && setup.BuildFailed {
		failure.Recovery, failure.Command = manualRecovery(p.local, p.revision, true)
	} else if !p.absent && failure.Command != "" {
		failure.Recovery, failure.Command = manualRecovery(p.local, p.revision, false)
	}
	return &FailureError{Failure: failure, Cause: err}
}

func (p *pluginPreparation) checkCurrent() error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if p.current == nil || !p.current() {
		return context.Canceled
	}
	return nil
}

func (p *pluginPreparation) plan(config []byte, guest herdr.GuestPluginState) (pluginPlan, error) {
	merged, err := mergeKeys(config, guest.Config, p.local.ID)
	if err != nil {
		return pluginPlan{}, err
	}
	arch, err := platformTarget(guest.Platform)
	if err != nil {
		return pluginPlan{}, err
	}
	manifest, doc, err := p.manifest()
	if err != nil {
		return pluginPlan{}, err
	}
	if guest.Plugin.ID != "" && !guest.Managed {
		return pluginPlan{}, errors.New("guest installation is managed outside herdr-sbx; left unchanged")
	}
	files, err := planPayload(p.local.Root, p.local.ManifestPath, doc, arch)
	if err != nil {
		return pluginPlan{}, err
	}
	commands, err := linuxBuildCommands(doc)
	if err != nil {
		return pluginPlan{}, err
	}
	files = append(files, payloadFile{"herdr-plugin.toml", manifest, 0600})
	version, _ := doc["version"].(string)
	return pluginPlan{files: files, commands: commands, config: merged, version: version, revision: payloadRevision(files, version, arch)}, nil
}

func (p *pluginPreparation) manifest() ([]byte, map[string]any, error) {
	manifest, err := readSmall(p.local.ManifestPath)
	if err != nil {
		return nil, nil, err
	}
	doc, err := parseConfig(manifest)
	if err != nil {
		return nil, nil, errors.New("invalid plugin manifest")
	}
	version, _ := doc["version"].(string)
	if doc["id"] != p.local.ID || version == "" || version != p.local.Version {
		return nil, nil, errors.New("plugin manifest does not match installed metadata")
	}
	platforms := keyNames(doc["platforms"])
	if len(platforms) > 0 && !slices.Contains(platforms, "linux") {
		return nil, nil, errors.New("plugin does not support Linux")
	}
	return manifest, doc, nil
}

func payloadRevision(files []payloadFile, version, arch string) string {
	// Content plus version/architecture identify an immutable guest distribution.
	var identity bytes.Buffer
	identity.WriteString(version + arch)
	for _, file := range files {
		fmt.Fprintf(&identity, "%s:%o:%s\n", file.name, file.mode, digest(file.data))
	}
	return digest(identity.Bytes())[:24]
}

func (p *pluginPreparation) repaired(guest herdr.GuestPluginState, plan pluginPlan) bool {
	return guest.Managed && strings.HasSuffix(guest.Plugin.Root, "/"+p.local.ID+"-"+p.revision) && guest.Plugin.Version == plan.version && guest.Plugin.Enabled && guest.Plugin.Available && noPluginWarnings(guest.Plugin)
}

func (p *pluginPreparation) apply(guest herdr.GuestPluginState, plan pluginPlan, attempts []BeginAttempt) (result error) {
	repaired := p.repaired(guest, plan)
	if repaired && bytes.Equal(plan.config, guest.Config) && guest.AppliedConfigSHA == digest(guest.Config) {
		return p.finish()
	}
	if err := p.checkCurrent(); err != nil {
		return err
	}
	if repaired {
		plan.commands = nil
		attempts = nil
	}
	var finishers []func(error) error
	defer func() { result = p.finishAttempts(result, finishers) }()
	if err := p.beginAttempts(attempts, &finishers); err != nil {
		return err
	}
	return p.install(guest, plan)
}

func (p *pluginPreparation) beginAttempts(attempts []BeginAttempt, finishers *[]func(error) error) error {
	for _, attempt := range attempts {
		if attempt == nil {
			continue
		}
		finish, err := attempt(p.local.ID, p.revision)
		if err != nil {
			return err
		}
		if finish != nil {
			*finishers = append(*finishers, finish)
		}
	}
	return nil
}

func (p *pluginPreparation) finishAttempts(result error, finishers []func(error) error) (finished error) {
	finished = result
	for _, finish := range finishers {
		defer func() {
			finished = p.failure(finished)
			completed := finish(finished)
			var structured *FailureError
			if errors.As(completed, &structured) {
				finished = completed
			} else {
				finished = errors.Join(finished, completed)
			}
		}()
	}
	return finished
}

func (p *pluginPreparation) install(guest herdr.GuestPluginState, plan pluginPlan) error {
	bundle, err := makeBundle(plan.files, plan.config)
	if err != nil {
		return err
	}
	if err := p.checkCurrent(); err != nil {
		return err
	}
	if err := p.client.InstallGuestPlugin(p.ctx, p.target, herdr.GuestPluginInstall{PluginID: p.local.ID, Bundle: bytes.NewReader(bundle), BundleSHA: digest(bundle), ConfigSHA: digest(guest.Config), Revision: p.revision, ConfigExists: guest.ConfigExists, BuildCommands: plan.commands}); err != nil {
		return err
	}
	if err := p.checkCurrent(); err != nil {
		return err
	}
	verified, err := p.client.InspectGuestPlugin(p.ctx, p.target, p.local.ID)
	if err != nil {
		return err
	}
	if verified.Plugin.ID != p.local.ID || !verified.Plugin.Enabled || verified.Plugin.Version != plan.version || !verified.Plugin.Available || !noPluginWarnings(verified.Plugin) || verified.AppliedConfigSHA != digest(plan.config) {
		return errors.New("guest plugin setup could not be verified")
	}
	return p.finish()
}

func (p *pluginPreparation) finish() error {
	return store.FinishGuestPluginAttempt(p.scope, p.sandboxID, p.local.ID, p.revision, "")
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
