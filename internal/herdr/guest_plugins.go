package herdr

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// PluginInstall is the installed plugin metadata exposed by Herdr.
type PluginInstall struct {
	ID           string         `json:"plugin_id"`
	Root         string         `json:"plugin_root"`
	ManifestPath string         `json:"manifest_path"`
	Version      string         `json:"version"`
	Enabled      bool           `json:"enabled"`
	Source       PluginSource   `json:"source"`
	Warnings     PluginWarnings `json:"warnings"`
	Available    bool           `json:"sbx_available"`
}

// PluginWarnings retains herdr's warnings without assuming their object schema.
type PluginWarnings string

func (w *PluginWarnings) UnmarshalJSON(data []byte) error { *w = PluginWarnings(data); return nil }
func (w PluginWarnings) MarshalJSON() ([]byte, error) {
	if w == "" {
		return []byte("null"), nil
	}
	return []byte(w), nil
}

// PluginSource identifies the exact GitHub revision supplied by herdr.
type PluginSource struct {
	Kind           string `json:"kind"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Subdir         string `json:"subdir"`
	ResolvedCommit string `json:"resolved_commit"`
}

func parsePlugins(output string) ([]PluginInstall, error) {
	var response struct {
		Result struct {
			Plugins []PluginInstall `json:"plugins"`
		} `json:"result"`
	}
	if err := decodeJSON(output, &response); err != nil {
		return nil, err
	}
	if response.Result.Plugins == nil {
		return nil, errors.New("herdr plugin list did not report plugins")
	}
	for _, plugin := range response.Result.Plugins {
		if strings.TrimSpace(plugin.ID) == "" {
			return nil, errors.New("herdr plugin list entry is missing its ID")
		}
	}
	return response.Result.Plugins, nil
}

func parsePluginInstall(output string) (PluginInstall, error) {
	rows, err := parsePlugins(output)
	if err != nil {
		return PluginInstall{}, err
	}
	if len(rows) == 0 {
		return PluginInstall{}, nil
	}
	if len(rows) != 1 {
		return PluginInstall{}, fmt.Errorf("ambiguous plugin inventory")
	}
	return rows[0], nil
}

// LocalPlugins returns the installed catalog, including locally disabled plugins.
func (c *Client) LocalPlugins(ctx context.Context) ([]PluginInstall, error) {
	output, err := c.command(ctx, 5*time.Second, true, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	return parsePlugins(output)
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

// ErrGuestPluginResponse identifies a malformed guest inspection response.
var ErrGuestPluginResponse = errors.New("unexpected guest plugin response")

// ErrGuestPluginConnection identifies a failed SSH transport or SSH launch,
// rather than a command failure after the connection was established.
var ErrGuestPluginConnection = errors.New("guest plugin inspection could not open a new SSH connection")

// GuestPluginConnectionError retains the connection cause separately from
// sanitized diagnostic text and a confirmed missing sbx host workspace.
type GuestPluginConnectionError struct {
	Cause             error
	Detail            string
	HostWorkspacePath string
}

func (e *GuestPluginConnectionError) Error() string {
	return ErrGuestPluginConnection.Error() + ": " + e.Detail
}
func (e *GuestPluginConnectionError) Unwrap() error { return e.Cause }
func (e *GuestPluginConnectionError) Is(target error) bool {
	return target == ErrGuestPluginConnection
}

var guestWorkspacePreflightFailure = regexp.MustCompile(`workspace directory "([^"\r\n]+)" no longer exists on the host(?:;|$)`)

const guestConfigPath = `config=${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}; `

const guestPluginFrameStart = "HERDR_SBX_PLUGIN_V1_BEGIN"
const guestPluginFrameEnd = "HERDR_SBX_PLUGIN_V1_END"

// InspectGuestPlugin observes guest dependencies, registration and configuration.
func (c *Client) InspectGuestPlugin(ctx context.Context, target, id string) (GuestPluginState, error) {
	if !validGuestPluginID.MatchString(id) {
		return GuestPluginState{}, fmt.Errorf("invalid plugin ID")
	}
	output, err := c.guestCommand(ctx, target, guestPluginInspectionScript(id))
	if err != nil {
		return GuestPluginState{}, guestPluginInspectionError(output, err)
	}
	return parseGuestPluginInspection(output)
}

func guestPluginInspectionScript(id string) string {
	return guestExecutable + guestConfigPath + `for tool in tar sha256sum base64 jq; do command -v "$tool" >/dev/null || { printf 'plugin requires %s in the sandbox\n' "$tool" >&2; exit 1; }; done
plugins=$("$h" --session default plugin list --plugin ` + quoteShell(id) + ` --json)
root=$(printf '%s' "$plugins" | jq -r '.result.plugins[0].plugin_root // ""')
manifest=$(printf '%s' "$plugins" | jq -r '.result.plugins[0].manifest_path // ""')
available=false
if [ -d "$root" ] && [ -r "$manifest" ] && [ -s "$manifest" ]; then available=true; fi
plugins=$(printf '%s' "$plugins" | jq --argjson available "$available" '.result.plugins |= map(. + {sbx_available: $available})')
printf '` + guestPluginFrameStart + `\n'
uname -sm
printf '%s' "$config" | base64 | tr -d '\n'; printf '\n'
if [ -f "$config" ]; then printf 'present\n'; base64 < "$config" | tr -d '\n'; else printf 'absent\n'; fi
printf '\n'
printf '%s' "$plugins" | base64 | tr -d '\n'; printf '\n'
managed=external
config_sha=""
base=$(CDPATH= cd -P "$HOME/.local/share/herdr-sbx/plugins" 2>/dev/null && pwd -P) || base=""
physical_root=""
if [ -n "$root" ]; then physical_root=$(CDPATH= cd -P "$root" 2>/dev/null && pwd -P) || physical_root=""; fi
if [ -n "$base" ] && [ -n "$physical_root" ]; then
case "$physical_root" in
 "$base/"*)
 relative_root=${physical_root#"$base/"}
 case "$relative_root" in
  */*) ;;
  ` + id + `-?*)
   managed=managed
   if [ -f "$physical_root/.herdr-sbx-config" ] && [ -f "$physical_root/.herdr-sbx-files" ] && (cd -P "$physical_root" && sha256sum -c .herdr-sbx-files >/dev/null 2>&1); then
    marker=$(cat "$physical_root/.herdr-sbx-config")
    case "$marker" in
     *[!0-9a-f]*|"") ;;
     *) if [ "${#marker}" -eq 64 ]; then config_sha=$marker; fi;;
    esac
   fi;;
 esac;;
esac
fi
printf '%s\n%s' "$managed" "$config_sha"
printf '\n` + guestPluginFrameEnd + `\n'`
}

func guestPluginInspectionError(output string, err error) error {
	for _, tool := range []string{"jq", "tar", "sha256sum", "base64"} {
		message := "plugin requires " + tool + " in the sandbox"
		if strings.TrimSpace(output) == message {
			return fmt.Errorf("%s", message)
		}
	}
	// A dropped connection can follow a partial response containing private
	// guest configuration. Retain only diagnostics before that frame.
	if start := strings.Index(output, guestPluginFrameStart); start >= 0 {
		output = output[:start]
	}
	detail := safePluginDiagnostic(output)
	if connection := guestPluginConnectionFailure(err, detail); connection != nil {
		return connection
	}
	if detail != "" {
		return fmt.Errorf("inspect guest plugin: %w: %s", err, detail)
	}
	return fmt.Errorf("inspect guest plugin: %w", err)
}

func guestPluginConnectionFailure(err error, detail string) error {
	var exitError *exec.ExitError
	var launchError *exec.Error
	workspaceFailure := guestWorkspacePreflightFailure.FindStringSubmatch(detail)
	failed := (errors.As(err, &exitError) && (exitError.ExitCode() == 255 || (exitError.ExitCode() == 1 && len(workspaceFailure) != 0))) || errors.As(err, &launchError)
	if !failed {
		return nil
	}
	connection := &GuestPluginConnectionError{Cause: err, Detail: detail}
	if len(workspaceFailure) != 0 {
		connection.HostWorkspacePath = workspaceFailure[1]
		connection.Detail = fmt.Sprintf("sbx refused a new SSH connection: saved host workspace path %q no longer exists", connection.HostWorkspacePath)
	} else if detail == "" {
		connection.Detail = safePluginDiagnostic(err.Error())
	}
	return connection
}

func parseGuestPluginInspection(output string) (GuestPluginState, error) {
	parts, err := guestPluginInspectionFields(output)
	if err != nil {
		return GuestPluginState{}, err
	}
	path, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return GuestPluginState{}, fmt.Errorf("%w: invalid config path encoding", ErrGuestPluginResponse)
	}
	config, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return GuestPluginState{}, fmt.Errorf("%w: invalid config encoding", ErrGuestPluginResponse)
	}
	plugins, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return GuestPluginState{}, fmt.Errorf("%w: invalid plugin catalog encoding", ErrGuestPluginResponse)
	}
	plugin, err := parsePluginInstall(string(plugins))
	if err != nil {
		return GuestPluginState{}, fmt.Errorf("%w: invalid plugin catalog", ErrGuestPluginResponse)
	}
	return GuestPluginState{Platform: parts[0], ConfigPath: string(path), Config: config, ConfigExists: parts[2] == "present", Plugin: plugin, Managed: parts[5] == "managed", AppliedConfigSHA: parts[6]}, nil
}

func guestPluginInspectionFields(output string) ([]string, error) {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	start := strings.LastIndex(output, guestPluginFrameStart+"\n")
	if start < 0 {
		return nil, fmt.Errorf("%w: response frame is missing", ErrGuestPluginResponse)
	}
	response := output[start+len(guestPluginFrameStart)+1:]
	end := strings.Index(response, "\n"+guestPluginFrameEnd)
	if end < 0 {
		return nil, fmt.Errorf("%w: response frame is incomplete", ErrGuestPluginResponse)
	}
	parts := strings.Split(response[:end], "\n")
	for len(parts) > 7 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) != 7 {
		return nil, fmt.Errorf("%w: expected 7 fields, got %d", ErrGuestPluginResponse, len(parts))
	}
	return parts, nil
}

// GuestPluginSetupError separates the command failure from safe guest output.
type GuestPluginSetupError struct {
	// BuildFailed is true only for the controlled pre-link build failure frame.
	BuildFailed bool
	Cause       error
	Output      string
}

func (e *GuestPluginSetupError) Error() string {
	return fmt.Sprintf("guest plugin setup: %v: %s", e.Cause, e.Output)
}
func (e *GuestPluginSetupError) Unwrap() error { return e.Cause }

// InstallGuestPlugin transfers an already verified host bundle. The remote
// transaction refuses modified installs, backs up config and checks its digest
// before replacement. Manifest build commands run in order inside the guest.
func (c *Client) InstallGuestPlugin(ctx context.Context, target, id string, bundle io.Reader, bundleSHA, configSHA, revision string, exists bool, buildCommands ...[]string) error {
	if !validGuestPluginID.MatchString(id) || !regexp.MustCompile(`^[a-zA-Z0-9.-]+$`).MatchString(revision) {
		return fmt.Errorf("invalid plugin revision")
	}
	expected := "absent"
	if exists {
		expected = configSHA
	}
	buildScript, err := guestPluginBuildScript(buildCommands)
	if err != nil {
		return err
	}
	script := guestExecutable + guestConfigPath + `umask 077
base="$HOME/.local/share/herdr-sbx/plugins"
mkdir -p "$base"
base=$(CDPATH= cd -P "$base" && pwd -P)
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
[ ! -L "$root" ] || { echo 'Refusing to replace a symlinked plugin directory' >&2; exit 1; }
if [ -e "$root" ]; then
 physical_root=$(CDPATH= cd -P "$root" && pwd -P) || { echo 'Managed plugin root is not a directory' >&2; exit 1; }
 [ "$physical_root" = "$root" ] || { echo 'Managed plugin directory resolves outside its revision root' >&2; exit 1; }
 # Keep this exact revision, including any manual repair; never overwrite it.
else
 mv "$stage/plugin" "$root"
fi
if [ ! -f "$root/.herdr-sbx-built" ]; then
` + buildScript + `
 touch "$root/.herdr-sbx-built"
fi
# Builds may replace tracked source files. Record the resulting files for inspection.
(cd "$root" && find . -type f ! -name ".herdr-sbx-*" -exec sha256sum {} \;) > "$stage/built.sha256"
cp "$stage/built.sha256" "$root/.herdr-sbx-files"
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
	output, err := c.runner.CombinedOutput(ctx, process.Command{Name: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, script}, Stdin: bundle, Timeout: 10 * time.Minute})
	if err != nil {
		const buildFailureFrame = "HERDR_SBX_PLUGIN_BUILD_FAILED\n"
		buildFailed := strings.HasPrefix(output, buildFailureFrame)
		if buildFailed {
			output = strings.TrimPrefix(output, buildFailureFrame)
		}
		return &GuestPluginSetupError{Cause: err, Output: safePluginDiagnostic(output), BuildFailed: buildFailed}
	}
	return nil
}

func guestPluginBuildScript(buildCommands [][]string) (string, error) {
	buildScript := ""
	for i, argv := range buildCommands {
		if len(argv) == 0 {
			return "", errors.New("empty guest build command")
		}
		quoted := make([]string, len(argv))
		for j, arg := range argv {
			if strings.ContainsRune(arg, 0) {
				return "", errors.New("invalid guest build argument")
			}
			quoted[j] = quoteShell(arg)
		}
		buildScript += fmt.Sprintf("(cd \"$root\" && %s) > \"$stage/build.log\" 2>&1 || { printf 'HERDR_SBX_PLUGIN_BUILD_FAILED\\n' >&2; printf 'Guest plugin build step %d failed\\n' >&2; printf 'Source kept at %%s\\n' \"$root\" >&2; tail -c 8192 \"$stage/build.log\" >&2; exit 1; }\n", strings.Join(quoted, " "), i+1)
	}
	return buildScript, nil
}

// safePluginDiagnostic keeps build output useful without retaining common
// credential forms or authenticated URL parameters in durable errors.
func safePluginDiagnostic(output string) string {
	if len(output) > 8192 {
		output = output[len(output)-8192:]
	}
	output = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`).ReplaceAllString(output, "${1}[redacted]@")
	output = regexp.MustCompile(`(https?://[^\s?]+)\?[^\s]+`).ReplaceAllString(output, "${1}?[redacted]")
	output = regexp.MustCompile(`(?im)([a-z0-9_]*(?:token|password|secret|api[_-]?key|authorization)[a-z0-9_]*\s*[:=]\s*)[^\r\n]+`).ReplaceAllString(output, "${1}[redacted]")
	return strings.TrimSpace(output)
}
