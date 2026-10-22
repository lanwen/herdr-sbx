package registration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/version"
)

const releaseManifestURL = "https://herdr.dev/latest.json"
const maxHerdrDownload = 256 << 20

// A transport seam lets tests serve the official URLs without external access.
type herdrRelease struct {
	Version  string                  `json:"version"`
	Assets   map[string]string       `json:"assets"`
	SHA256   map[string]string       `json:"sha256"`
	Releases map[string]herdrRelease `json:"releases"`
}

func (s *Service) prepareFromHost(ctx context.Context, target string, current func() bool, progress func(string)) (string, error) {
	local, err := s.herdr.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("read local Herdr version: %w", err)
	}
	wanted := version.Parse(local)
	if strings.TrimSpace(local) != "herdr "+wanted || wanted == "" {
		return "", fmt.Errorf("local Herdr must report an exact stable release version")
	}
	return s.prepareHostVersion(ctx, target, wanted, "", current, progress)
}

// An expected old version permits atomic replacement of only the user-local
// executable we just inspected. Other paths and symlinks are left untouched.
func (s *Service) prepareHostVersion(ctx context.Context, target, wanted, old string, current func() bool, progress func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current != nil && !current() {
			return context.Canceled
		}
		return nil
	}
	if err := check(); err != nil {
		return "", err
	}
	output, err := s.runSSH(ctx, 15*time.Second, target, "uname -sm")
	if err != nil {
		return "", fmt.Errorf("sandbox architecture: %s", commandError(err, output))
	}
	var platform string
	switch strings.TrimSpace(output) {
	case "Linux x86_64":
		platform = "linux-x86_64"
	case "Linux aarch64", "Linux arm64":
		platform = "linux-aarch64"
	default:
		return "", fmt.Errorf("unsupported sandbox platform: %q", strings.TrimSpace(output))
	}
	if err := check(); err != nil {
		return "", err
	}
	if progress != nil {
		progress("Downloading Herdr on the host for " + platform + ".")
	}
	client := &http.Client{Transport: s.releaseTransport, Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many release redirects")
		}
		u := req.URL
		if u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "github.com" && u.Hostname() != "release-assets.githubusercontent.com" && u.Hostname() != "herdr.dev") {
			return fmt.Errorf("unexpected release redirect host")
		}
		return nil
	}}
	manifest, err := releaseGet(ctx, client, releaseManifestURL)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(manifest, (2<<20)+1))
	manifest.Close()
	if err != nil {
		return "", err
	}
	if len(data) > 2<<20 {
		return "", fmt.Errorf("release manifest exceeds size limit")
	}
	var release herdrRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("invalid release manifest: %w", err)
	}
	if release.Version != wanted {
		archived, ok := release.Releases[wanted]
		if !ok {
			return "", fmt.Errorf("release manifest has no verified assets for local Herdr %s", wanted)
		}
		release = archived
		release.Version = wanted
	}
	if version.Parse(release.Version) != release.Version || version.Compare(release.Version, minHerdrVersion) < 0 {
		return "", fmt.Errorf("release manifest has an incompatible version")
	}
	asset, err := url.Parse(release.Assets[platform])
	if err != nil || asset.Scheme != "https" || asset.Host != "github.com" || asset.User != nil || asset.RawQuery != "" || asset.Fragment != "" || asset.Path != "/herdrdev/herdr/releases/download/v"+release.Version+"/herdr-"+platform {
		return "", fmt.Errorf("release manifest has an unexpected asset URL")
	}
	digest, err := hex.DecodeString(release.SHA256[platform])
	if err != nil || len(digest) != sha256.Size {
		return "", fmt.Errorf("release manifest lacks a valid SHA256")
	}
	if err := check(); err != nil {
		return "", err
	}
	body, err := releaseGet(ctx, client, asset.String())
	if err != nil {
		return "", err
	}
	defer body.Close()
	file, err := os.CreateTemp("", "herdr-sbx-download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, maxHerdrDownload+1))
	if err != nil {
		return "", fmt.Errorf("host Herdr download: %w", err)
	}
	if size == 0 || size > maxHerdrDownload {
		return "", fmt.Errorf("Herdr download has an invalid size")
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if checksum != strings.ToLower(release.SHA256[platform]) {
		return "", fmt.Errorf("host Herdr download SHA256 mismatch")
	}
	if err := check(); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if progress != nil {
		progress("Copying verified Herdr into the sandbox over SSH.")
	}
	if err := s.streamHerdrReplacing(ctx, target, file, checksum, release.Version, old); err != nil {
		return "", err
	}
	if err := check(); err != nil {
		return "", err
	}
	status, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return "", err
	}
	if status.Missing || status.Conflict || version.Parse(status.Version) != release.Version {
		return "", fmt.Errorf("transferred Herdr did not pass the remote readiness check")
	}
	return status.Version, nil
}

func releaseGet(ctx context.Context, client *http.Client, address string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("host Herdr download: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("host Herdr download returned HTTP %d", response.StatusCode)
	}
	return response.Body, nil
}

func (s *Service) streamHerdr(ctx context.Context, target string, binary io.Reader, checksum, version string) error {
	return s.streamHerdrReplacing(ctx, target, binary, checksum, version, "")
}

func (s *Service) streamHerdrReplacing(ctx context.Context, target string, binary io.Reader, checksum, version, old string) error {
	if err := CheckAutomaticWork(ctx); err != nil {
		return err
	}
	script := transferredInstallCommand(checksum, version)
	if old != "" {
		script = strings.Replace(script, `[ ! -e "$HOME/.local/bin/herdr" ] && [ ! -L "$HOME/.local/bin/herdr" ]; ln -T "$stage/herdr" "$HOME/.local/bin/herdr"`,
			`[ ! -L "$HOME/.local/bin/herdr" ]; [ "$(command -v herdr || printf '%s' "$HOME/.local/bin/herdr")" = "$HOME/.local/bin/herdr" ]; [ "$("$HOME/.local/bin/herdr" --version)" = 'herdr `+old+`' ]; mv -f "$stage/herdr" "$HOME/.local/bin/herdr"`, 1)
	}
	output, err := s.runner.CombinedOutput(ctx, process.Command{
		Name: "ssh", Stdin: binary,
		Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, script},
	})
	if err != nil {
		return fmt.Errorf("SSH Herdr transfer: %s", commandError(err, string(output)))
	}
	return nil
}

// Digest and version are validated before they reach this shell command.
func transferredInstallCommand(checksum, version string) string {
	return `set -eu; umask 077; mkdir -p "$HOME/.local/share" "$HOME/.local/bin"; stage=$(mktemp -d "$HOME/.local/share/herdr-sbx-install.XXXXXX"); trap 'rm -rf "$stage"' EXIT; trap 'exit 1' HUP INT TERM; cat > "$stage/herdr"; printf '%s  %s\n' '` + checksum + `' "$stage/herdr" | sha256sum -c -; chmod 755 "$stage/herdr"; installed_version=$("$stage/herdr" --version); [ "$installed_version" = 'herdr ` + version + `' ]; [ ! -e "$HOME/.local/bin/herdr" ] && [ ! -L "$HOME/.local/bin/herdr" ]; ln -T "$stage/herdr" "$HOME/.local/bin/herdr"`
}
