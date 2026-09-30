package registration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/version"
)

const missingRemoteHerdr = "__herdr_missing__"
const remoteHerdrOnPath = "__herdr_on_path__"
const remoteHerdrFallback = "__herdr_fallback__"
const remoteHerdrConflict = "__herdr_conflict__"

type remoteHerdr struct {
	Version  string
	Missing  bool
	OnPath   bool
	Conflict bool
}

func (s *Service) remoteHerdrStatus(ctx context.Context, target string) (remoteHerdr, error) {
	command := "if command -v herdr >/dev/null 2>&1; then printf '" + remoteHerdrOnPath + "\\n'; herdr --version; elif [ -x \"$HOME/.local/bin/herdr\" ]; then printf '" + remoteHerdrFallback + "\\n'; \"$HOME/.local/bin/herdr\" --version; elif [ -e \"$HOME/.local/bin/herdr\" ] || [ -L \"$HOME/.local/bin/herdr\" ]; then printf '" + remoteHerdrConflict + "\\n'; else printf '" + missingRemoteHerdr + "\\n'; fi"
	output, err := s.runSSH(ctx, 15*time.Second, target, command)
	if err != nil {
		return remoteHerdr{}, fmt.Errorf("remote Herdr check: %s", commandError(err, output))
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || lines[0] == missingRemoteHerdr {
		return remoteHerdr{Missing: true}, nil
	}
	if lines[0] == remoteHerdrConflict {
		return remoteHerdr{Conflict: true}, nil
	}
	if lines[0] != remoteHerdrOnPath && lines[0] != remoteHerdrFallback {
		return remoteHerdr{}, fmt.Errorf("remote Herdr check returned an unrecognised response")
	}
	return remoteHerdr{Version: strings.Join(lines[1:], "\n"), OnPath: lines[0] == remoteHerdrOnPath}, nil
}

func (s *Service) prepareMissingRemote(ctx context.Context, target string, current func() bool, progress func(string)) (string, error) {
	if current != nil && !current() {
		return "", context.Canceled
	}
	if progress != nil {
		progress("Installing missing Herdr on the sandbox.")
	}
	command := remoteInstallCommand()
	output, err := s.runSSH(ctx, 90*time.Second, target, command)
	if err != nil {
		return "", fmt.Errorf("remote Herdr install: %s", commandError(err, output))
	}
	if current != nil && !current() {
		return "", context.Canceled
	}
	status, err := s.remoteHerdrStatus(ctx, target)
	if err != nil {
		return "", err
	}
	if status.Missing {
		return "", fmt.Errorf("installed Herdr is not discoverable through the sandbox SSH command")
	}
	installedVersion := version.Parse(status.Version)
	if installedVersion == "" || version.Compare(installedVersion, minHerdrVersion) < 0 {
		return "", fmt.Errorf("installed Herdr is not compatible")
	}
	return status.Version, nil
}

func remoteInstallCommand() string {
	return "set -eu; mkdir -p \"$HOME/.local/share\" \"$HOME/.local/bin\"; stage=$(mktemp -d \"$HOME/.local/share/herdr-sbx-install.XXXXXX\"); trap 'rm -rf \"$stage\"' EXIT; curl --fail --location --silent --show-error --max-time 60 https://herdr.dev/install.sh -o \"$stage/install.sh\"; HERDR_INSTALL_DIR=\"$stage\" sh \"$stage/install.sh\"; installed_version=$(\"$stage/herdr\" --version); printf '%s\\n' \"$installed_version\" | awk -v minimum='" + minHerdrVersion + "' 'BEGIN { ok=0; split(minimum,m,\".\") } $1 == \"herdr\" { split($2,v,\".\"); for(i=1;i<=3;i++) { if(v[i]+0 > m[i]+0) { ok=1; break }; if(v[i]+0 < m[i]+0) break; if(i==3) ok=1 } } END { exit !ok }'; [ ! -e \"$HOME/.local/bin/herdr\" ]; ln \"$stage/herdr\" \"$HOME/.local/bin/herdr\""
}
