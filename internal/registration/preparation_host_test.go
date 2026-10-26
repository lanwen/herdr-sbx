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
	"runtime"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

type releaseTestTransport func(*http.Request) (*http.Response, error)

func (f releaseTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHostPreparation(t *testing.T) {
	for _, scenario := range []string{"success amd64", "success arm64", "success archived", "missing archived", "corrupt download", "host denied", "bad asset", "incompatible manifest", "transfer corruption", "broken executable", "transfer failure", "replacement", "canceled", "existing file", "existing symlink"} {
		t.Run(scenario, func(t *testing.T) {
			testHostPreparationScenario(t, scenario)
		})
	}
}

func TestMissingRemoteHerdrIsPreparedBeforeRegistration(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	binary := []byte("#!/bin/sh\necho herdr 0.9.3\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	assetURL := "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64"
	commands := &missingHerdrCommands{digest: digest, binary: binary}
	fixture.runner.Execute = commands.execute
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
	require.NoError(t, fixture.service.Reconcile(&output))
	require.True(t, commands.installed)
	require.True(t, commands.added)
	wantOrder := []string{"probe-missing", "transfer", "verify", "add"}
	require.Equal(t, wantOrder, commands.events, "events=%v want %v", commands.events, wantOrder)
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
	require.NoError(t, os.Symlink(ln, filepath.Join(tools, "ln")))
	bin := t.TempDir()
	fakeSSH := `#!/bin/sh
set -eu
[ "$1" = -o ] && [ "$2" = BatchMode=yes ] && [ "$3" = -o ] && [ "$4" = ConnectTimeout=8 ] && [ "$5" = fixture.sbx ]
exec /bin/sh -c "$6"
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0700))
	path := bin + ":" + tools + ":/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"
	t.Setenv("PATH", path)
	t.Setenv("HOME", home)
	destination := filepath.Join(home, ".local/bin/herdr")
	service := &Service{runner: process.Runner{}}
	binary := "#!/bin/sh\necho herdr 0.9.3\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(binary)))
	{
		err := service.streamHerdr(context.Background(), "fixture.sbx", strings.NewReader("truncated"), digest, "0.9.3")
		require.Error(t, err, "accepted damaged stdin")
	}
	require.NoError(t, service.streamHerdr(context.Background(), "fixture.sbx", strings.NewReader(binary), digest, "0.9.3"))
	contents, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.False(t, string(contents) != binary)
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
		require.Contains(t, command, required, "remote installer missing %q", required)
	}
	require.NotContains(t, command, "ln -s")
	require.NotContains(t, command, "curl --insecure")
}

// Execute replacement scripts against an isolated HOME, including a concurrent
// version change and a PATH installation outside the plugin's install directory.
func TestHostTransferReplacement(t *testing.T) {
	for _, scenario := range []string{"replace", "changed", "symlink", "other path"} {
		t.Run(scenario, func(t *testing.T) {
			testHostTransferReplacement(t, scenario)
		})
	}
}

func testHostPreparationScenario(t *testing.T, scenario string) {
	t.Helper()

	home, tools := t.TempDir(), t.TempDir()
	ln := "/bin/ln"
	if runtime.GOOS == "darwin" {
		var err error
		ln, err = exec.LookPath("gln")
		if err != nil {
			t.Skip("Linux shell fixture requires GNU ln on macOS")
		}
	}
	require.NoError(t, os.Symlink(ln, filepath.Join(tools, "ln")))
	fixture := newRegistrationFixture(t, "[]")
	destination := filepath.Join(home, ".local/bin/herdr")
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
	if scenario == "existing file" {
		require.NoError(t, os.WriteFile(destination, []byte("original"), 0700))
	}
	if scenario == "existing symlink" {
		require.NoError(t, os.Symlink("missing", destination))
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
	commands := &hostPreparationCommands{t: t, scenario: scenario, platform: platform, architecture: architecture, digest: digest, binary: binary, home: home, tools: tools, destination: destination, cancel: cancel, current: true}
	releaseTransport := releaseTestTransport(commands.roundTrip)
	fixture.runner.Execute = commands.execute
	fixture.rebuildClients()
	fixture.service.releaseTransport = releaseTransport
	_, err := fixture.service.prepareFromHost(ctx, "demo.sbx", func() bool { return commands.current }, nil)
	assertHostPreparation(t, scenario, err, destination, home, binary, commands)
}

func testHostTransferReplacement(t *testing.T, scenario string) {
	t.Helper()

	home := t.TempDir()
	destination := filepath.Join(home, ".local/bin/herdr")
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
	original := "#!/bin/sh\necho herdr 0.9.4\n"
	if scenario == "changed" {
		original = "#!/bin/sh\necho herdr 0.9.5\n"
	}
	require.NoError(t, os.WriteFile(destination, []byte(original), 0700))
	if scenario == "symlink" {
		require.NoError(t, os.Rename(destination, destination+"-real"))
		require.NoError(t, os.Symlink(destination+"-real", destination))
	}
	tools := filepath.Dir(destination)
	if scenario == "other path" {
		tools = t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tools, "herdr"), []byte(original), 0700))
	}
	runner := process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		return runInstallerCommand(ctx, command.Args[len(command.Args)-1], command.Stdin, home, tools)
	}}
	service := &Service{runner: runner}
	binary := "#!/bin/sh\necho herdr 0.9.3\n"
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(binary)))
	err := service.streamHerdrReplacing(context.Background(), "fixture.sbx", strings.NewReader(binary), checksum, "0.9.3", "0.9.4")
	contents, readErr := os.ReadFile(destination)
	require.NoError(t, readErr)
	if scenario == "replace" {
		require.NoError(t, err)
		require.False(t, string(contents) != binary)
	} else if err == nil || string(contents) != original {
		t.Fatalf("changed protected executable: %q %v", contents, err)
	}
}

type hostPreparationCommands struct {
	t                                                                  *testing.T
	scenario, platform, architecture, digest, home, tools, destination string
	binary                                                             []byte
	cancel                                                             context.CancelFunc
	current                                                            bool
	downloads, transfers                                               int
}

func (f *hostPreparationCommands) roundTrip(request *http.Request) (*http.Response, error) {

	var status int = http.StatusOK
	var contents []byte
	switch request.URL.String() {
	case releaseManifestURL:
		contents = f.manifest()
	case "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-" + f.platform:
		status, contents = f.download()
	default:
		f.t.Fatalf("unexpected download %s", request.URL)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(contents))), Request: request}, nil
}
func (f *hostPreparationCommands) manifest() []byte {

	version := "0.9.3"
	if f.scenario == "incompatible manifest" {
		version = "0.1.0"
	}
	asset := "https://github.com/herdrdev/herdr/releases/download/v" + version + "/herdr-" + f.platform
	if f.scenario == "bad asset" {
		asset = "https://example.org/herdr"
	}
	release := herdrRelease{Version: version, Assets: map[string]string{f.platform: asset}, SHA256: map[string]string{f.platform: f.digest}}
	if f.scenario == "success archived" || f.scenario == "missing archived" {
		previous := release
		release.Version = "0.9.4"
		release.Assets = map[string]string{f.platform: "https://github.com/herdrdev/herdr/releases/download/v0.9.4/herdr-" + f.platform}
		if f.scenario == "success archived" {
			release.Releases = map[string]herdrRelease{"0.9.3": previous}
		}
	}
	manifest, _ := json.Marshal(release)
	return manifest
}
func (f *hostPreparationCommands) download() (status int, contents []byte) {
	status = http.StatusOK

	f.downloads++
	if f.scenario == "host denied" {
		status = http.StatusForbidden
	} else if f.scenario == "corrupt download" {
		contents = []byte("bad")
	} else {
		contents = f.binary
	}
	if f.scenario == "replacement" {
		f.current = false
	}
	if f.scenario == "canceled" {
		f.cancel()
	}
	return
}
func (f *hostPreparationCommands) execute(ctx context.Context, command process.Command) (string, error) {

	if command.Name == "herdr" && strings.Join(command.Args, " ") == "--version" {
		return "herdr 0.9.3", nil
	}
	if command.Name != "ssh" || len(command.Args) == 0 {
		return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
	}
	remote := command.Args[len(command.Args)-1]
	switch remote {
	case "uname -sm":
		return f.architecture, nil
	case remoteHerdrStatusShell():
		output, err := exec.CommandContext(ctx, f.destination, "--version").CombinedOutput()
		if err != nil {
			return "", err
		}
		return remoteHerdrOnPath + "\n" + strings.TrimSpace(string(output)) + "\n", nil
	default:
		return f.transfer(ctx, remote, command.Stdin)
	}
}
func (f *hostPreparationCommands) transfer(ctx context.Context, remote string, binary io.Reader) (string, error) {

	f.transfers++
	if f.scenario == "transfer failure" {
		return "", fmt.Errorf("test SSH failure")
	}
	input := binary
	if f.scenario == "transfer corruption" {
		input = strings.NewReader("corrupt")
	}
	return runInstallerCommand(ctx, remote, input, f.home, f.tools)
}
func assertHostPreparation(t *testing.T, scenario string, err error, destination, home string, binary []byte, f *hostPreparationCommands) {
	t.Helper()
	success := strings.HasPrefix(scenario, "success")
	require.Equal(t, success, (err == nil), "error=%v", err)
	require.False(t, scenario == "host denied" && (!strings.Contains(err.Error(), "host Herdr download returned HTTP 403") || strings.Contains(err.Error(), "policy")), "wrong guidance: %v", err)
	data, readErr := os.ReadFile(destination)
	switch {
	case success:
		require.NoError(t, readErr)
		require.False(t, string(data) != string(binary))
	case scenario == "existing file":
		require.Equal(t, "original", string(data), "existing binary changed")
	case scenario == "existing symlink":
		link, e := os.Readlink(destination)
		require.NoError(t, e)
		require.EqualValues(t, "missing", link)
	default:
		require.True(t, os.IsNotExist(readErr), "failed install published %q", data)
	}
	stages, _ := filepath.Glob(filepath.Join(home, ".local/share/herdr-sbx-install.*"))
	require.Len(t, stages, 0, "staging files leaked: %v", stages)
	if scenario == "replacement" || scenario == "canceled" || scenario == "corrupt download" || scenario == "host denied" {
		require.Equal(t, 0, f.transfers, "transferred unverified/stale binary")
	}
	if scenario == "bad asset" || scenario == "incompatible manifest" || scenario == "missing archived" {
		require.Equal(t, 0, f.downloads, "downloaded invalid release")
	}
}

func TestHostDownloadPanicClosesAndRemovesPartialFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	body := &panickingHostDownload{}
	client := &http.Client{Transport: releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: request}, nil
	})}
	require.Panics(t, func() { _, _, _ = downloadHerdr(context.Background(), client, "https://github.com/test", "unused") })
	require.True(t, body.closed)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

type panickingHostDownload struct{ closed bool }

func (d *panickingHostDownload) Read([]byte) (int, error) { panic("interrupted download") }
func (d *panickingHostDownload) Close() error             { d.closed = true; return nil }

type missingHerdrCommands struct {
	installed, added bool
	events           []string
	digest           string
	binary           []byte
}

func (f *missingHerdrCommands) execute(_ context.Context, command process.Command) (string, error) {

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
		if f.installed {
			f.events = append(f.events, "verify")
			return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
		}
		f.events = append(f.events, "probe-missing")
		return missingRemoteHerdr + "\n", nil
	case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx uname -sm":
		return "Linux x86_64\n", nil
	case "herdr machine list --json":
		if !f.added {
			return `[]`, nil
		}
		return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
	case "herdr machine add --label sbx · demo demo.sbx":
		f.events = append(f.events, "add")
		f.added = true
		return "Saved SSH machine machine-1. Remote server is ready.\n", nil
	case "herdr --machine machine-1 status server":
		return "running\n", nil
	}
	if command.Name == "ssh" && len(command.Args) > 0 && command.Args[len(command.Args)-1] == transferredInstallCommand(f.digest, "0.9.3") {
		return f.transfer(command.Stdin)
	}
	return "", fmt.Errorf("unexpected command: %s %s", command.Name, joined)
}
func (f *missingHerdrCommands) transfer(input io.Reader) (string, error) {
	transferred, err := io.ReadAll(input)
	if err != nil {
		return "", err
	}
	if string(transferred) != string(f.binary) {
		return "", fmt.Errorf("transferred binary mismatch: %q", transferred)
	}
	f.events = append(f.events, "transfer")
	f.installed = true
	return "", nil
}

func TestSuccessfulHostDownloadClosePanicRemovesFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	payload := "verified executable"
	digest := sha256.Sum256([]byte(payload))
	body := &closePanicHostDownload{Reader: strings.NewReader(payload)}
	client := &http.Client{Transport: releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: request}, nil
	})}
	require.PanicsWithValue(t, "response close interrupted", func() {
		_, _, _ = downloadHerdr(context.Background(), client, "https://github.com/test", fmt.Sprintf("%x", digest))
	})
	require.Equal(t, 1, body.closes)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

type closePanicHostDownload struct {
	io.Reader
	closes int
}

func (d *closePanicHostDownload) Close() error { d.closes++; panic("response close interrupted") }
