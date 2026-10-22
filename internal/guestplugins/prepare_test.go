package guestplugins

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func guestInspectionResponse(t *testing.T, plugins []herdr.PluginInstall, ownership, checksum string) string {
	t.Helper()
	catalog, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": plugins}})
	require.NoError(t, err)
	return "HERDR_SBX_PLUGIN_V1_BEGIN\nLinux aarch64\n" + base64.StdEncoding.EncodeToString([]byte("/home/guest/config.toml")) + "\nabsent\n\n" + base64.StdEncoding.EncodeToString(catalog) + "\n" + ownership + "\n" + checksum + "\nHERDR_SBX_PLUGIN_V1_END\n"
}

func TestManuallyLinkedRetainedRevisionDoesNotRebuild(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
	root := t.TempDir()
	manifest := filepath.Join(root, "herdr-plugin.toml")
	os.WriteFile(manifest, []byte("id='example.plugin'\nversion='1.0.0'\n[[build]]\ncommand=['sh','build.sh']\n"), 0600)
	os.WriteFile(filepath.Join(root, "build.sh"), []byte("exit 0"), 0700)
	local := herdr.PluginInstall{ID: "example.plugin", Version: "1.0.0", Root: root, ManifestPath: manifest}
	revision := ""
	linked := false
	transferred := false
	client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if cmd.Stdin != nil {
			require.NotContains(t, cmd.Args[len(cmd.Args)-1], "&& 'sh' 'build.sh'", "manual repair rebuilt")
			transferred = true
			return "", nil
		}
		plugin := herdr.PluginInstall{}
		checksum := ""
		if linked {
			plugin = herdr.PluginInstall{ID: local.ID, Version: local.Version, Root: "/home/guest/.local/share/herdr-sbx/plugins/" + local.ID + "-" + revision, Enabled: true, Available: true}
		}
		if transferred {
			checksum = digest(nil)
		}
		rows := []herdr.PluginInstall{}
		if linked {
			rows = append(rows, plugin)
		}
		ownership := "external"
		if linked {
			ownership = "managed"
		}
		return guestInspectionResponse(t, rows, ownership, checksum), nil
	}})
	block := func(id, rev string) (func(error) error, error) {
		revision = rev
		return nil, errors.New("failed previous build")
	}

	err := prepare(context.Background(), client, "fixture.sbx", "scope", "sandbox", local, func() bool { return true }, block)
	require.Error(t, err, "attempt unexpectedly allowed")

	linked = true
	denied := func(string, string) (func(error) error, error) {
		t.Fatal("manual repair retried build gate")
		return nil, nil
	}
	require.NoError(t, prepare(context.Background(), client, "fixture.sbx", "scope", "sandbox", local, func() bool { return true }, denied))
	require.True(t, transferred, "config completion not performed")
}

func TestExactExternalPluginRequiresAvailableWarningFreeInstallation(t *testing.T) {
	source := herdr.PluginSource{Kind: "github", Owner: "owner", Repo: "repo", ResolvedCommit: strings.Repeat("a", 40)}
	local := herdr.PluginInstall{ID: "example.plugin", Version: "1", Source: source}
	guest := local
	guest.Enabled = true
	guest.Available = true
	require.True(t, exactExternalPlugin(local, guest), "matching installation rejected")
	guest.Warnings = herdr.PluginWarnings(`["manifest missing"]`)
	require.False(t, exactExternalPlugin(local, guest), "warning accepted")
	guest.Warnings = ""
	guest.Available = false
	require.False(t, exactExternalPlugin(local, guest), "missing installation accepted")
}

// Build scripts can read the manifest as text. Copying it must preserve the
// quoting and comments they use, even when its canonical filename changes.
func TestPreparePreservesManifestForGuestBuild(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
	root := t.TempDir()
	manifest := []byte("# source metadata\nid = \"example.source\"\nversion = \"1.2.3\"\n[[build]]\ncommand = [\"sh\", \"build.sh\"]\n")
	os.WriteFile(filepath.Join(root, "source.toml"), manifest, 0600)
	os.WriteFile(filepath.Join(root, "build.sh"), []byte(`version=$(sed -n 's/^version *= *"\([^"]*\)".*/\1/p' herdr-plugin.toml)
[ "$version" = 1.2.3 ] || { echo "invalid release version: $version"; exit 1; }
`), 0700)
	local := herdr.PluginInstall{ID: "example.source", Version: "1.2.3", Root: root, ManifestPath: filepath.Join(root, "source.toml")}
	stop := errors.New("stop after guest build")
	built := false
	client := herdr.New(process.Runner{Execute: func(ctx context.Context, cmd process.Command) (string, error) {
		if cmd.Stdin == nil {
			return guestInspectionResponse(t, []herdr.PluginInstall{}, "external", ""), nil
		}
		guest := t.TempDir()
		tr := tar.NewReader(cmd.Stdin)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if !strings.HasPrefix(h.Name, "plugin/") {
				continue
			}
			path := filepath.Join(guest, strings.TrimPrefix(h.Name, "plugin/"))
			os.MkdirAll(filepath.Dir(path), 0700)
			data, _ := io.ReadAll(tr)
			require.NoError(t, os.WriteFile(path, data, os.FileMode(h.Mode)))
		}
		command := exec.CommandContext(ctx, "sh", "build.sh")
		command.Dir = guest
		output, err := command.CombinedOutput()
		assert.NoError(t, err, "guest build: %s: %v", output, err)
		copied, _ := os.ReadFile(filepath.Join(guest, "herdr-plugin.toml"))
		assert.Equal(t, manifest, copied, "guest manifest was rewritten")
		built = true
		return "", stop
	}})
	err := prepare(context.Background(), client, "fixture.sbx", "scope", "sandbox", local, func() bool { return true })
	require.True(t, built)
	require.ErrorIs(t, err, stop)
}

func TestPrepareOnlySuggestsInstallBeforeGuestLink(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "herdr-plugin.toml")
	os.WriteFile(manifest, []byte("id=\"example.repair\"\nversion=\"1.0.0\"\n[[build]]\ncommand=[\"sh\",\"build.sh\"]\n"), 0600)
	os.WriteFile(filepath.Join(root, "build.sh"), []byte("exit 1\n"), 0700)
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."}, {"-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "source"}} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "git source: %s", output)
	}
	headCommand := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	head, err := headCommand.Output()
	require.NoError(t, err)
	local := herdr.PluginInstall{ID: "example.repair", Version: "1.0.0", Root: root, ManifestPath: manifest, Source: herdr.PluginSource{Kind: "github", Owner: "example", Repo: "repair", ResolvedCommit: strings.TrimSpace(string(head))}}
	for _, test := range []struct {
		name                          string
		buildFailed, existing, verify bool
		wantCommand                   bool
	}{
		{name: "build before link", buildFailed: true, wantCommand: true},
		{name: "transport outcome unknown"},
		{name: "post-link inspection", verify: true},
		{name: "update of linked plugin", buildFailed: true, existing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))

			transferred := false
			client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
				if cmd.Stdin != nil {
					transferred = true
					if test.verify {
						return "", nil
					}
					output := "link or connection failed"
					if test.buildFailed {
						output = "HERDR_SBX_PLUGIN_BUILD_FAILED\ncompiler missing"
					}
					return output, errors.New("setup failed")
				}
				if transferred && test.verify {
					return "", errors.New("post-link inspection failed")
				}
				plugins := []herdr.PluginInstall{}
				managed := "external"
				if test.existing {
					plugins = append(plugins, herdr.PluginInstall{ID: local.ID, Version: local.Version, Root: "/home/guest/.local/share/herdr-sbx/plugins/example.repair-old", Enabled: true, Available: true})
					managed = "managed"
				}
				return guestInspectionResponse(t, plugins, managed, ""), nil
			}})
			err := prepare(context.Background(), client, "fixture.sbx", "scope", "id", local, func() bool { return true })
			var failure *FailureError
			require.True(t, errors.As(err, &failure), "missing structured failure: %v", err)
			require.Equal(t, test.wantCommand, (failure.Failure.Command != ""), "repair command validity: %+v", failure.Failure)
			require.False(t, test.wantCommand && !strings.Contains(failure.Failure.Command, strings.TrimSpace(string(head))), "unpinned command: %+v", failure.Failure)
			require.False(t, !test.wantCommand && !strings.Contains(failure.Failure.Recovery, "~/.local/share/herdr-sbx/plugins/"), "missing retained root: %+v", failure.Failure)
		})
	}
}
