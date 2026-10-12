package herdr

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// PluginInstall is the installed plugin metadata exposed by Herdr.
type PluginInstall struct {
	ID, Root, Version string
	Enabled           bool
}

func parsePluginInstall(output string) (PluginInstall, error) {
	rows, err := objects(output, "plugins")
	if err != nil {
		return PluginInstall{}, err
	}
	if len(rows) == 0 {
		return PluginInstall{}, nil
	}
	if len(rows) != 1 {
		return PluginInstall{}, fmt.Errorf("ambiguous plugin inventory")
	}
	r := rows[0]
	enabled, _ := r["enabled"].(bool)
	return PluginInstall{ID: first(r, "plugin_id", "id"), Root: first(r, "plugin_root"), Version: first(r, "version"), Enabled: enabled}, nil
}

// LocalPlugins returns the installed catalog, including locally disabled plugins.
func (c *Client) LocalPlugins(ctx context.Context) ([]PluginInstall, error) {
	output, err := c.command(ctx, 5*time.Second, true, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	rows, err := objects(output, "plugins")
	if err != nil {
		return nil, err
	}
	result := make([]PluginInstall, 0, len(rows))
	for _, row := range rows {
		enabled, _ := row["enabled"].(bool)
		result = append(result, PluginInstall{ID: first(row, "plugin_id", "id"), Root: first(row, "plugin_root"), Version: first(row, "version"), Enabled: enabled})
	}
	return result, nil
}

// GuestPluginState contains only the observations needed to prepare plugin.
// Config is kept in memory and must not be included in logs or error messages.
type GuestPluginState struct {
	Platform, ConfigPath string
	Config               []byte
	ConfigExists         bool
	Plugin               PluginInstall
	AppliedConfigSHA     string
	Managed              bool
}

var validGuestPluginID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

const guestConfigPath = `config=${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}; `

// InspectGuestPlugin observes guest dependencies, registration and configuration.
func (c *Client) InspectGuestPlugin(ctx context.Context, target, id string) (GuestPluginState, error) {
	if !validGuestPluginID.MatchString(id) {
		return GuestPluginState{}, fmt.Errorf("invalid plugin ID")
	}
	requiredTools := "tar sha256sum base64 jq"
	if id == "persiyanov.reviewr" {
		requiredTools += " bash git"
	}
	script := guestExecutable + guestConfigPath + `for tool in ` + requiredTools + `; do command -v "$tool" >/dev/null || { printf 'plugin requires %s in the sandbox\n' "$tool" >&2; exit 1; }; done
plugins=$("$h" --session default plugin list --plugin ` + quoteShell(id) + ` --json)
uname -sm
printf '%s' "$config" | base64 | tr -d '\n'; printf '\n'
if [ -f "$config" ]; then printf 'present\n'; base64 < "$config" | tr -d '\n'; else printf 'absent\n'; fi
printf '\n'
printf '%s' "$plugins" | base64 | tr -d '\n'; printf '\n'
root=$(printf '%s' "$plugins" | jq -r '(if type == "object" and has("result") then .result else . end) | (if type == "array" then . else .plugins end) | .[0].plugin_root // ""')
legacy=""
if [ ` + quoteShell(id) + ` = persiyanov.reviewr ]; then legacy="$HOME/.local/share/herdr-sbx/plugins/reviewr-"; fi
case "$root" in
 "$HOME/.local/share/herdr-sbx/plugins/` + id + `-"*|"${legacy:-/nonexistent-legacy-plugin/}"*)
 printf 'managed\n'
 if [ -f "$root/.herdr-sbx-config" ] && [ -f "$root/.herdr-sbx-files" ] && (cd "$root" && sha256sum -c .herdr-sbx-files >/dev/null 2>&1); then cat "$root/.herdr-sbx-config"; fi;;
 *) printf 'external\n';;
esac
printf '\n'`
	output, err := c.guestCommand(ctx, target, script)
	if err != nil {
		for _, tool := range []string{"jq", "tar", "sha256sum", "base64", "bash", "git"} {
			message := "plugin requires " + tool + " in the sandbox"
			if strings.TrimSpace(output) == message {
				return GuestPluginState{}, fmt.Errorf("%s", message)
			}
		}
		return GuestPluginState{}, fmt.Errorf("inspect guest plugin: ensure jq, tar, sha256sum and base64 are installed: %w", err)
	}
	parts := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(parts) != 7 {
		return GuestPluginState{}, fmt.Errorf("unexpected guest plugin response")
	}
	path, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return GuestPluginState{}, err
	}
	config, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return GuestPluginState{}, err
	}
	plugins, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return GuestPluginState{}, err
	}
	plugin, err := parsePluginInstall(string(plugins))
	if err != nil {
		return GuestPluginState{}, err
	}
	return GuestPluginState{Platform: parts[0], ConfigPath: string(path), Config: config, ConfigExists: parts[2] == "present", Plugin: plugin, Managed: parts[5] == "managed", AppliedConfigSHA: parts[6]}, nil
}

// InstallGuestPlugin transfers an already verified host bundle. The remote
// transaction refuses modified installs, backs up config and checks its digest
// before replacement. Herdr links a build-free manifest: no guest downloads.
func (c *Client) InstallGuestPlugin(ctx context.Context, target, id string, bundle io.Reader, bundleSHA, configSHA, revision string, exists bool) error {
	if !validGuestPluginID.MatchString(id) || !regexp.MustCompile(`^[a-zA-Z0-9.-]+$`).MatchString(revision) {
		return fmt.Errorf("invalid plugin revision")
	}
	expected := "absent"
	if exists {
		expected = configSHA
	}
	script := guestExecutable + guestConfigPath + `umask 077
base="$HOME/.local/share/herdr-sbx/plugins"
mkdir -p "$base"
stage=$(mktemp -d "$base/.plugin.XXXXXX")
trap 'rm -rf "$stage"' EXIT
trap 'exit 1' HUP INT TERM
cat > "$stage/bundle.tar"
printf '%s  %s\n' ` + quoteShell(bundleSHA) + ` "$stage/bundle.tar" | sha256sum -c - >/dev/null
tar -xf "$stage/bundle.tar" -C "$stage"
HERDR_CONFIG_PATH="$stage/config.toml" "$h" config check >/dev/null 2>&1 || { echo 'Guest config validation failed' >&2; exit 1; }
[ ! -L "$config" ] || { echo 'Refusing to replace a symlinked Herdr config' >&2; exit 1; }
actual=absent
if [ -f "$config" ]; then actual=$(sha256sum "$config"); actual=${actual%% *}; fi
[ "$actual" = ` + quoteShell(expected) + ` ] || { echo 'Herdr config changed; retry setup' >&2; exit 1; }
root="$base/` + id + `-` + revision + `"
if [ -e "$root" ]; then
 (cd "$root" && sha256sum -c "$stage/files.sha256" >/dev/null) || { echo 'Managed plugin files changed; refusing overwrite' >&2; exit 1; }
else
 mv "$stage/plugin" "$root"
fi
cp "$stage/files.sha256" "$root/.herdr-sbx-files"
"$h" --session default plugin link "$root" >/dev/null 2>&1 || { echo 'Guest plugin link failed' >&2; exit 1; }
"$h" --session default plugin enable ` + quoteShell(id) + ` >/dev/null 2>&1 || { echo 'Guest plugin enable failed' >&2; exit 1; }
[ ! -L "$config" ] || exit 1
actual=absent
if [ -f "$config" ]; then actual=$(sha256sum "$config"); actual=${actual%% *}; fi
[ "$actual" = ` + quoteShell(expected) + ` ] || { echo 'Herdr config changed; retry setup' >&2; exit 1; }
if ! cmp -s "$stage/config.toml" "$config"; then
 mkdir -p "$(dirname "$config")"
 if [ -e "$config" ]; then cp -p "$config" "$config.herdr-sbx-` + id + `.bak"; fi
 next=$(mktemp "${config}.herdr-sbx.XXXXXX")
 cat "$stage/config.toml" > "$next"
 mv "$next" "$config"
fi
"$h" --session default server reload-config >/dev/null 2>&1 || { echo 'Guest config reload failed; retry registration' >&2; exit 1; }
sha=$(sha256sum "$config"); printf '%s' "${sha%% *}" > "$root/.herdr-sbx-config"`
	output, err := c.runner.CombinedOutput(ctx, process.Command{Name: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, script}, Stdin: bundle, Timeout: 60 * time.Second})
	if err != nil {
		return fmt.Errorf("guest plugin setup: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}
