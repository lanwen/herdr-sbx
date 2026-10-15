package registration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

type releaseTestTransport func(*http.Request) (*http.Response, error)

func (f releaseTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHostPreparation(t *testing.T) {
	for _, scenario := range []string{"success amd64", "success arm64", "success archived", "missing archived", "corrupt download", "host denied", "bad asset", "incompatible manifest", "transfer corruption", "broken executable", "transfer failure", "replacement", "canceled", "existing file", "existing symlink"} {
		t.Run(scenario, func(t *testing.T) {
			home, tools := t.TempDir(), t.TempDir()
			ln := "/bin/ln"
			if runtime.GOOS == "darwin" {
				var err error
				ln, err = exec.LookPath("gln")
				if err != nil {
					t.Skip("Linux shell fixture requires GNU ln on macOS")
				}
			}
			if err := os.Symlink(ln, filepath.Join(tools, "ln")); err != nil {
				t.Fatal(err)
			}
			fixture := newRegistrationFixture(t, "[]")
			destination := filepath.Join(home, ".local/bin/herdr")
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				t.Fatal(err)
			}
			if scenario == "existing file" {
				if err := os.WriteFile(destination, []byte("original"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "existing symlink" {
				if err := os.Symlink("missing", destination); err != nil {
					t.Fatal(err)
				}
			}
			binary := []byte("#!/bin/sh\necho herdr 0.9.3\n")
			if scenario == "broken executable" {
				binary = append(binary, []byte("exit 1\n")...)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(binary))
			platform, architecture := "linux-x86_64", "Linux x86_64"
			if scenario == "success arm64" {
				platform, architecture = "linux-aarch64", "Linux aarch64"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			current := true
			downloads, transfers := 0, 0
			releaseTransport := releaseTestTransport(func(request *http.Request) (*http.Response, error) {
				var status int = http.StatusOK
				var contents []byte
				switch request.URL.String() {
				case releaseManifestURL:
					version := "0.9.3"
					if scenario == "incompatible manifest" {
						version = "0.1.0"
					}
					asset := "https://github.com/herdrdev/herdr/releases/download/v" + version + "/herdr-" + platform
					if scenario == "bad asset" {
						asset = "https://example.org/herdr"
					}
					release := herdrRelease{Version: version, Assets: map[string]string{platform: asset}, SHA256: map[string]string{platform: digest}}
					if scenario == "success archived" || scenario == "missing archived" {
						previous := release
						release.Version = "0.9.4"
						release.Assets = map[string]string{platform: "https://github.com/herdrdev/herdr/releases/download/v0.9.4/herdr-" + platform}
						if scenario == "success archived" {
							release.Releases = map[string]herdrRelease{"0.9.3": previous}
						}
					}
					manifest, _ := json.Marshal(release)
					contents = manifest
				case "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-" + platform:
					downloads++
					if scenario == "host denied" {
						status = http.StatusForbidden
					} else if scenario == "corrupt download" {
						contents = []byte("bad")
					} else {
						contents = binary
					}
					if scenario == "replacement" {
						current = false
					}
					if scenario == "canceled" {
						cancel()
					}
				default:
					t.Fatalf("unexpected download %s", request.URL)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(contents))), Request: request}, nil
			})
			fixture.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
				if command.Name == "herdr" && strings.Join(command.Args, " ") == "--version" {
					return "herdr 0.9.3", nil
				}
				if command.Name != "ssh" || len(command.Args) == 0 {
					return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
				}
				remote := command.Args[len(command.Args)-1]
				switch remote {
				case "uname -sm":
					return architecture, nil
				case remoteHerdrStatusShell():
					output, err := exec.CommandContext(ctx, destination, "--version").CombinedOutput()
					if err != nil {
						return "", err
					}
					return remoteHerdrOnPath + "\n" + strings.TrimSpace(string(output)) + "\n", nil
				default:
					transfers++
					if scenario == "transfer failure" {
						return "", fmt.Errorf("test SSH failure")
					}
					input := command.Stdin
					if scenario == "transfer corruption" {
						input = strings.NewReader("corrupt")
					}
					return runInstallerCommand(ctx, remote, input, home, tools)
				}
			}
			fixture.rebuildClients()
			fixture.service.releaseTransport = releaseTransport
			_, err := fixture.service.prepareFromHost(ctx, "demo.sbx", func() bool { return current }, nil)
			success := strings.HasPrefix(scenario, "success")
			if (err == nil) != success {
				t.Fatalf("error=%v", err)
			}
			if scenario == "host denied" && (!strings.Contains(err.Error(), "host Herdr download returned HTTP 403") || strings.Contains(err.Error(), "policy")) {
				t.Fatalf("wrong guidance: %v", err)
			}
			data, readErr := os.ReadFile(destination)
			switch {
			case success:
				if readErr != nil || string(data) != string(binary) {
					t.Fatalf("published %q: %v", data, readErr)
				}
			case scenario == "existing file":
				if string(data) != "original" {
					t.Fatal("existing binary changed")
				}
			case scenario == "existing symlink":
				link, e := os.Readlink(destination)
				if e != nil || link != "missing" {
					t.Fatal("existing symlink changed")
				}
			default:
				if !os.IsNotExist(readErr) {
					t.Fatalf("failed install published %q", data)
				}
			}
			stages, _ := filepath.Glob(filepath.Join(home, ".local/share/herdr-sbx-install.*"))
			if len(stages) != 0 {
				t.Fatalf("staging files leaked: %v", stages)
			}
			if scenario == "replacement" || scenario == "canceled" || scenario == "corrupt download" || scenario == "host denied" {
				if transfers != 0 {
					t.Fatal("transferred unverified/stale binary")
				}
			}
			if scenario == "bad asset" || scenario == "incompatible manifest" || scenario == "missing archived" {
				if downloads != 0 {
					t.Fatal("downloaded invalid release")
				}
			}
		})
	}
}

func TestMissingRemoteHerdrIsPreparedBeforeRegistration(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	binary := []byte("#!/bin/sh\necho herdr 0.9.3\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	assetURL := "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64"
	installed := false
	added := false
	var events []string
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		joined := strings.Join(command.Args, " ")
		switch command.Name + " " + joined {
		case "herdr --version":
			return "herdr 0.9.3\n", nil
		case "sbx version":
			return "sbx version: 0.45.1\n", nil
		case "ssh -V":
			return "OpenSSH_9.9\n", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json\n", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/test-sandboxd.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			if installed {
				events = append(events, "verify")
				return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
			}
			events = append(events, "probe-missing")
			return missingRemoteHerdr + "\n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx uname -sm":
			return "Linux x86_64\n", nil
		case "herdr machine list --json":
			if !added {
				return `[]`, nil
			}
			return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			events = append(events, "add")
			added = true
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr --machine machine-1 status server":
			return "running\n", nil
		}
		if command.Name == "ssh" && len(command.Args) > 0 && command.Args[len(command.Args)-1] == transferredInstallCommand(digest, "0.9.3") {
			transferred, err := io.ReadAll(command.Stdin)
			if err != nil {
				return "", err
			}
			if string(transferred) != string(binary) {
				return "", fmt.Errorf("transferred binary mismatch: %q", transferred)
			}
			events = append(events, "transfer")
			installed = true
			return "", nil
		}
		return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
	}
	fixture.rebuildClients()
	fixture.service.releaseTransport = releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		var contents []byte
		switch request.URL.String() {
		case releaseManifestURL:
			manifest, _ := json.Marshal(herdrRelease{
				Version: "0.9.3",
				Assets:  map[string]string{"linux-x86_64": assetURL},
				SHA256:  map[string]string{"linux-x86_64": digest},
			})
			contents = manifest
		case assetURL:
			contents = binary
		default:
			return nil, fmt.Errorf("unexpected release request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(contents))), Request: request}, nil
	})

	var output strings.Builder
	if err := fixture.service.Reconcile(&output); err != nil {
		t.Fatal(err)
	}
	if !installed || !added {
		t.Fatalf("installed=%t added=%t output=%s", installed, added, output.String())
	}
	wantOrder := []string{"probe-missing", "transfer", "verify", "add"}
	if !reflect.DeepEqual(events, wantOrder) {
		t.Fatalf("events=%v want %v", events, wantOrder)
	}
}

func TestSSHTransferStreamsVerifiedBytes(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	ln := "/bin/ln"
	if runtime.GOOS == "darwin" {
		var err error
		ln, err = exec.LookPath("gln")
		if err != nil {
			t.Skip("Linux shell fixture requires GNU ln on macOS")
		}
	}
	if err := os.Symlink(ln, filepath.Join(tools, "ln")); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fakeSSH := `#!/bin/sh
set -eu
[ "$1" = -o ] && [ "$2" = BatchMode=yes ] && [ "$3" = -o ] && [ "$4" = ConnectTimeout=8 ] && [ "$5" = fixture.sbx ]
exec /bin/sh -c "$6"
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0700); err != nil {
		t.Fatal(err)
	}
	path := bin + ":" + tools + ":/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"
	t.Setenv("PATH", path)
	t.Setenv("HOME", home)
	destination := filepath.Join(home, ".local/bin/herdr")
	service := &Service{runner: process.Runner{}}
	binary := "#!/bin/sh\necho herdr 0.9.3\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(binary)))
	if err := service.streamHerdr(context.Background(), "fixture.sbx", strings.NewReader("truncated"), digest, "0.9.3"); err == nil {
		t.Fatal("accepted damaged stdin")
	}
	if err := service.streamHerdr(context.Background(), "fixture.sbx", strings.NewReader(binary), digest, "0.9.3"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != binary {
		t.Fatalf("transferred %q: %v", contents, err)
	}
}

func runInstallerCommand(ctx context.Context, shell string, input io.Reader, home, tools string) (string, error) {
	command := exec.CommandContext(ctx, "/bin/sh", "-c", shell)
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+tools+":/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin")
	command.Stdin = input
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestRemoteInstallerShellPublishesOnlyAfterSuccess(t *testing.T) {
	command := remoteInstallCommand()
	for _, required := range []string{"set -eu", "curl --fail", "HERDR_INSTALL_DIR=\"$stage\"", "[ ! -e \"$HOME/.local/bin/herdr\" ]", "ln \"$stage/herdr\" \"$HOME/.local/bin/herdr\""} {
		if !strings.Contains(command, required) {
			t.Fatalf("remote installer missing %q", required)
		}
	}
	if strings.Contains(command, "ln -s") || strings.Contains(command, "curl --insecure") {
		t.Fatalf("unsafe remote installer: %s", command)
	}
}

// Execute replacement scripts against an isolated HOME, including a concurrent
// version change and a PATH installation outside the plugin's install directory.
func TestHostTransferReplacement(t *testing.T) {
	for _, scenario := range []string{"replace", "changed", "symlink", "other path"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			destination := filepath.Join(home, ".local/bin/herdr")
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				t.Fatal(err)
			}
			original := "#!/bin/sh\necho herdr 0.9.4\n"
			if scenario == "changed" {
				original = "#!/bin/sh\necho herdr 0.9.5\n"
			}
			if err := os.WriteFile(destination, []byte(original), 0700); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				if err := os.Rename(destination, destination+"-real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(destination+"-real", destination); err != nil {
					t.Fatal(err)
				}
			}
			tools := filepath.Dir(destination)
			if scenario == "other path" {
				tools = t.TempDir()
				if err := os.WriteFile(filepath.Join(tools, "herdr"), []byte(original), 0700); err != nil {
					t.Fatal(err)
				}
			}
			runner := process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
				return runInstallerCommand(ctx, command.Args[len(command.Args)-1], command.Stdin, home, tools)
			}}
			service := &Service{runner: runner}
			binary := "#!/bin/sh\necho herdr 0.9.3\n"
			checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(binary)))
			err := service.streamHerdrReplacing(context.Background(), "fixture.sbx", strings.NewReader(binary), checksum, "0.9.3", "0.9.4")
			contents, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "replace" {
				if err != nil || string(contents) != binary {
					t.Fatalf("replacement: %q %v", contents, err)
				}
			} else if err == nil || string(contents) != original {
				t.Fatalf("changed protected executable: %q %v", contents, err)
			}
		})
	}
}
