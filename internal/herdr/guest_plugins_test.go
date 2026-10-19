package herdr

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestInspectGuestPluginOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, root, marker string
		modified, managed  bool
		verified           bool
		symlink            bool
	}{
		{name: "canonical", root: ".local/share/herdr-sbx/plugins/example.plugin-revision", managed: true},
		{name: "verified canonical", root: ".local/share/herdr-sbx/plugins/example.plugin-revision", marker: strings.Repeat("a", 64), managed: true, verified: true},
		{name: "canonical invalid marker", root: ".local/share/herdr-sbx/plugins/example.plugin-revision", marker: "not-a-checksum", managed: true},
		{name: "canonical modified files", root: ".local/share/herdr-sbx/plugins/example.plugin-revision", marker: strings.Repeat("a", 64), modified: true, managed: true},
		{name: "noncanonical", root: ".local/share/herdr-sbx/plugins/manual", marker: strings.Repeat("a", 64)},
		{name: "external", root: "external/example.plugin-revision", marker: strings.Repeat("a", 64)},
		{name: "canonical symlink outside", root: ".local/share/herdr-sbx/plugins/example.plugin-revision", marker: strings.Repeat("a", 64), symlink: true},
		{name: "canonical traversal outside", root: ".local/share/herdr-sbx/plugins/example.plugin-revision/../../outside", marker: strings.Repeat("a", 64)},
		{name: "traversal to noncanonical private root", root: ".local/share/herdr-sbx/plugins/example.plugin-revision/../manual", marker: strings.Repeat("a", 64)},
		{name: "nested root", root: ".local/share/herdr-sbx/plugins/example.plugin-revision/manual", marker: strings.Repeat("a", 64)},
		{name: "missing revision", root: ".local/share/herdr-sbx/plugins/example.plugin-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			// Preserve traversal components in the catalog path seen by inspection.
			root := home + "/" + tc.root
			if err := os.MkdirAll(filepath.Join(home, ".local/share/herdr-sbx/plugins/example.plugin-revision"), 0700); err != nil {
				t.Fatal(err)
			}
			files := root
			if tc.symlink {
				files = filepath.Join(home, "external")
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(files, root); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(files, 0700); err != nil {
				t.Fatal(err)
			}
			payload := []byte("plugin payload")
			for name, content := range map[string]string{
				"payload":           string(payload),
				".herdr-sbx-config": tc.marker,
				".herdr-sbx-files":  fmt.Sprintf("%x  payload\n", sha256.Sum256(payload)),
			} {
				if name != "payload" && tc.marker == "" {
					continue
				}
				if err := os.WriteFile(filepath.Join(files, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.modified {
				if err := os.WriteFile(filepath.Join(root, "payload"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := guestInspectionShell(t, home, root, "")
			state, err := client.InspectGuestPlugin(context.Background(), "test.sbx", "example.plugin")
			if err != nil {
				t.Fatal(err)
			}
			wantSHA := ""
			if tc.verified {
				wantSHA = tc.marker
			}
			if state.Managed != tc.managed || state.AppliedConfigSHA != wantSHA {
				t.Fatalf("managed=%v sha=%q, want managed=%v sha=%q", state.Managed, state.AppliedConfigSHA, tc.managed, wantSHA)
			}
			actual, err := os.ReadFile(filepath.Join(root, "payload"))
			if err != nil || string(actual) != map[bool]string{false: "plugin payload", true: "changed"}[tc.modified] {
				t.Fatalf("inspection changed guest files: %q, %v", actual, err)
			}
		})
	}
}

func TestInspectGuestPluginProtocolDependencies(t *testing.T) {
	for _, missing := range []string{"", "tar", "sha256sum", "base64", "jq"} {
		t.Run("missing_"+missing, func(t *testing.T) {
			client := guestInspectionShell(t, t.TempDir(), "", missing)
			// This inspector does not need plugin-specific runtime dependencies.
			_, err := client.InspectGuestPlugin(context.Background(), "test.sbx", "example.plugin")
			if missing == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != "plugin requires "+missing+" in the sandbox" {
				t.Fatalf("missing dependency: %v", err)
			}
		})
	}
}

func TestInspectGuestPluginKeepsCommandFailureCause(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
		exitCode           string
	}{
		{"connection", "ssh: connect to host demo.sbx port 22: Connection refused", "Connection refused", "255"},
		{"guest command", "herdr: guest server is unavailable", "guest server is unavailable", "1"},
		{"sanitized", "ssh: failed https://user:private@host/path?token=private", "https://[redacted]@host/path?[redacted]", "255"},
		{"partial response", "connection interrupted\n" + guestPluginFrameStart + "\nprivate-guest-config", "connection interrupted", "255"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := exec.Command("/bin/sh", "-c", "exit "+tc.exitCode).Run()
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return tc.output, cause
			}})
			_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
			if err == nil || !errors.Is(err, cause) || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "ensure jq") || strings.Contains(err.Error(), "private") {
				t.Fatalf("missing or unsafe inspection cause: %v", err)
			}
			if errors.Is(err, ErrGuestPluginConnection) != (tc.exitCode == "255") {
				t.Fatalf("incorrect SSH connection classification: %v", err)
			}
		})
	}
}

func TestInspectGuestPluginDoesNotCallCommandTimeoutAConnectionFailure(t *testing.T) {
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return "", context.DeadlineExceeded
	}})
	_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrGuestPluginConnection) {
		t.Fatalf("ambiguous command timeout classified as connection failure: %v", err)
	}
}

func TestInspectGuestPluginRecognizesSandboxWorkspacePreflightFailure(t *testing.T) {
	cause := exec.Command("/bin/sh", "-c", "exit 1").Run()
	const output = `Error: workspace directory "/Users/test/project/sbxenv.yaml" no longer exists on the host; restore the directory, or remove the sandbox with: sbx rm demo`
	for _, framed := range []bool{false, true} {
		t.Run(fmt.Sprintf("framed_%t", framed), func(t *testing.T) {
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				if framed {
					return guestPluginFrameStart + "\n" + output, cause
				}
				return output, cause
			}})
			_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
			wantConnectionCause := !framed
			var connection *GuestPluginConnectionError
			if errors.Is(err, ErrGuestPluginConnection) != wantConnectionCause || !errors.Is(err, cause) || errors.As(err, &connection) != wantConnectionCause {
				t.Fatalf("incorrect sandbox connection preflight classification: %v", err)
			}
			if wantConnectionCause && (connection.HostWorkspacePath != "/Users/test/project/sbxenv.yaml" || strings.Contains(err.Error(), "sbx rm")) {
				t.Fatalf("missing or misleading structured workspace cause: %v", err)
			}
		})
	}
}

func TestInspectGuestPluginSymlinkTraversalStaysExternal(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".local/share/herdr-sbx/plugins")
	outside := filepath.Join(home, "external")
	for _, dir := range []string{filepath.Join(base, "example.plugin-owned"), filepath.Join(outside, "child"), filepath.Join(outside, "example.plugin-owned")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"payload":           "payload",
			".herdr-sbx-config": strings.Repeat("a", 64),
			".herdr-sbx-files":  fmt.Sprintf("%x  payload\n", sha256.Sum256([]byte("payload"))),
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	link := filepath.Join(base, "example.plugin-link-out")
	if err := os.Symlink(filepath.Join(outside, "child"), link); err != nil {
		t.Fatal(err)
	}
	// Kernel path lookup reaches outside; logical cd would reach a canonical private root.
	client := guestInspectionShell(t, home, link+"/../example.plugin-owned", "")
	state, err := client.InspectGuestPlugin(context.Background(), "test.sbx", "example.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if state.Managed || state.AppliedConfigSHA != "" {
		t.Fatalf("outside symlink traversal classified as managed: %#v", state)
	}
}

// Run the generated guest shell in an isolated home with only protocol tools.
func guestInspectionShell(t *testing.T, home, root, missing string) *Client {
	t.Helper()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"tar", "sha256sum", "base64", "jq", "uname", "tr", "cat"} {
		if tool == missing {
			continue
		}
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("guest fixture needs %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	plugins := `{"result":{"plugins":[]}}`
	if root != "" {
		data, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": []map[string]string{{"plugin_id": "example.plugin", "plugin_root": root}}}})
		if err != nil {
			t.Fatal(err)
		}
		plugins = string(data)
	}
	if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\nprintf '%s\\n' "+quoteShell(plugins)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1])
		cmd.Env = []string{"HOME=" + home, "PATH=" + bin}
		output, err := cmd.CombinedOutput()
		return string(output), err
	}})
}

func TestInspectGuestPluginIgnoresSSHStartupOutput(t *testing.T) {
	path := base64.StdEncoding.EncodeToString([]byte("/tmp/herdr/config.toml"))
	plugins := base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[]}}`))
	response := "Welcome to the sandbox\nHERDR_SBX_PLUGIN_V1_BEGIN\nLinux x86_64\n" + path + "\nabsent\n\n" + plugins + "\nexternal\n\nHERDR_SBX_PLUGIN_V1_END\nlogout message\n"
	client := New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		if cmd.Name != "ssh" || !strings.Contains(cmd.Args[len(cmd.Args)-1], guestPluginFrameStart) || !strings.Contains(cmd.Args[len(cmd.Args)-1], guestPluginFrameEnd) {
			t.Fatalf("unexpected guest command: %s", cmd.Name)
		}
		return response, nil
	}})
	state, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
	if err != nil {
		t.Fatal(err)
	}
	if state.Platform != "Linux x86_64" || state.ConfigPath != "/tmp/herdr/config.toml" || state.ConfigExists || state.Managed {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestInspectGuestPluginReportsMalformedFrameWithoutContent(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString([]byte("private guest config"))
	response := guestPluginFrameStart + "\nLinux x86_64\n" + secret + "\nabsent\n\nexternal\n" + guestPluginFrameEnd + "\n"
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return response, nil
	}})
	_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
	if !errors.Is(err, ErrGuestPluginResponse) || !strings.Contains(err.Error(), "expected 7 fields, got 5") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe or unhelpful malformed response error: %v", err)
	}
}

func TestInspectGuestPluginAcceptsTrailingChecksumNewline(t *testing.T) {
	path := base64.StdEncoding.EncodeToString([]byte("/tmp/herdr/config.toml"))
	plugins := base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[]}}`))
	response := guestPluginFrameStart + "\nLinux x86_64\n" + path + "\nabsent\n\n" + plugins + "\nmanaged\nchecksum\n\n" + guestPluginFrameEnd + "\n"
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return response, nil
	}})
	state, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
	if err != nil || !state.Managed || state.AppliedConfigSHA != "checksum" {
		t.Fatalf("checksum with trailing newline: state=%#v err=%v", state, err)
	}
}

func TestInspectGuestPluginClassifiesMalformedFields(t *testing.T) {
	for _, field := range []int{1, 3, 4, 5} {
		t.Run(fmt.Sprint(field), func(t *testing.T) {
			parts := []string{"Linux x86_64", base64.StdEncoding.EncodeToString([]byte("/config")), "absent", "", base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[]}}`)), "external", ""}
			if field == 5 {
				parts[4] = base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[null]}}`))
			} else {
				parts[field] = "private-invalid-base64%%%"
			}
			client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
				return guestPluginFrameStart + "\n" + strings.Join(parts, "\n") + "\n" + guestPluginFrameEnd + "\n", nil
			}})
			_, err := client.InspectGuestPlugin(context.Background(), "demo.sbx", "example.plugin")
			if !errors.Is(err, ErrGuestPluginResponse) || strings.Contains(err.Error(), "private-invalid") {
				t.Fatalf("malformed response classification/content: %v", err)
			}
		})
	}
}

func TestInstallGuestPluginRequiresCanonicalDirectory(t *testing.T) {
	for _, kind := range []string{"directory", "external symlink", "internal symlink", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			base := filepath.Join(home, ".local/share/herdr-sbx/plugins")
			root := filepath.Join(base, "example.plugin-revision")
			actual := root
			if kind == "external symlink" || kind == "dangling symlink" {
				actual = filepath.Join(home, "outside")
			} else if kind == "internal symlink" {
				actual = filepath.Join(base, "other")
			}
			if err := os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			if kind != "dangling symlink" {
				if err := os.MkdirAll(actual, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(actual, "payload"), []byte("payload"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if actual != root {
				if err := os.Symlink(actual, root); err != nil {
					t.Fatal(err)
				}
			}
			bin := filepath.Join(home, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOME/herdr-calls\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var bundle bytes.Buffer
			writer := tar.NewWriter(&bundle)
			for name, data := range map[string]string{"plugin/payload": "payload", "files.sha256": fmt.Sprintf("%x  payload\n", sha256.Sum256([]byte("payload"))), "config.toml": ""} {
				if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))}); err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			client := New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1])
				cmd.Env = []string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "HERDR_CONFIG_PATH=" + filepath.Join(home, "config.toml")}
				cmd.Stdin = command.Stdin
				output, err := cmd.CombinedOutput()
				return string(output), err
			}})
			digest := fmt.Sprintf("%x", sha256.Sum256(bundle.Bytes()))
			err := client.InstallGuestPlugin(context.Background(), "test.sbx", "example.plugin", &bundle, digest, "", "revision", false)
			if kind == "directory" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(root, ".herdr-sbx-config")); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted a symlink as the managed revision directory")
			}
			for _, marker := range []string{".herdr-sbx-files", ".herdr-sbx-config"} {
				if _, err := os.Stat(filepath.Join(actual, marker)); !os.IsNotExist(err) {
					t.Fatalf("wrote marker outside revision directory: %s, %v", marker, err)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(home, "herdr-calls"))
			if strings.Contains(string(calls), "plugin link") || strings.Contains(string(calls), "plugin enable") {
				t.Fatalf("linked plugin from symlink: %s", calls)
			}
		})
	}
}

func TestGuestBuildRunsOnceAndKeepsFailedSource(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			home := t.TempDir()
			bin := filepath.Join(home, "bin")
			os.Mkdir(bin, 0700)
			os.WriteFile(filepath.Join(bin, "herdr"), []byte("#!/bin/sh\nexit 0\n"), 0700)
			var archive bytes.Buffer
			tw := tar.NewWriter(&archive)
			for name, data := range map[string]string{"plugin/source": "source", "files.sha256": fmt.Sprintf("%x  source\n", sha256.Sum256([]byte("source"))), "config.toml": ""} {
				tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))})
				tw.Write([]byte(data))
			}
			tw.Close()
			payload := append([]byte(nil), archive.Bytes()...)
			client := New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
				cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1])
				cmd.Stdin = command.Stdin
				cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"), "HERDR_CONFIG_PATH="+filepath.Join(home, "config.toml"))
				out, err := cmd.CombinedOutput()
				return string(out), err
			}})
			build := []string{"sh", "-c", "printf x >> count; cp source artifact"}
			if fail {
				build = []string{"sh", "-c", "printf 'TOKEN=secret-value\\nnetwork denied\\n'; exit 1"}
			}
			invoke := func(exists bool) error {
				configSHA := ""
				if exists {
					configSHA = fmt.Sprintf("%x", sha256.Sum256(nil))
				}
				return client.InstallGuestPlugin(context.Background(), "fixture.sbx", "example.plugin", bytes.NewReader(payload), fmt.Sprintf("%x", sha256.Sum256(payload)), configSHA, "revision", exists, build)
			}
			err := invoke(false)
			root := filepath.Join(home, ".local/share/herdr-sbx/plugins/example.plugin-revision")
			if fail {
				if err == nil || !strings.Contains(err.Error(), "build step 1 failed") || !strings.Contains(err.Error(), "network denied") || strings.Contains(err.Error(), "secret-value") {
					t.Fatalf("diagnostic: %v", err)
				}
				if data, err := os.ReadFile(filepath.Join(root, "source")); err != nil || string(data) != "source" {
					t.Fatalf("failed build source lost: %q %v", data, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := invoke(true); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(filepath.Join(root, "count")); err != nil || string(data) != "x" {
				t.Fatalf("built more than once: %q %v", data, err)
			}
		})
	}
}

func TestGuestPluginAvailabilityChecksRetainedRegistryPaths(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "external")
	os.Mkdir(root, 0700)
	manifest := filepath.Join(root, "herdr-plugin.toml")
	os.WriteFile(manifest, []byte("id='example.plugin'"), 0600)
	client := guestInspectionShell(t, home, root, "")
	plugins, _ := json.Marshal(map[string]any{"result": map[string]any{"plugins": []map[string]any{{"plugin_id": "example.plugin", "plugin_root": root, "manifest_path": manifest, "warnings": []string{}}}}})
	os.WriteFile(filepath.Join(home, "bin", "herdr"), []byte("#!/bin/sh\nprintf '%s\\n' "+quoteShell(string(plugins))+"\n"), 0700)
	state, err := client.InspectGuestPlugin(context.Background(), "fixture.sbx", "example.plugin")
	if err != nil || !state.Plugin.Available {
		t.Fatalf("available=%v err=%v", state.Plugin.Available, err)
	}
	os.Remove(manifest)
	state, err = client.InspectGuestPlugin(context.Background(), "fixture.sbx", "example.plugin")
	if err != nil || state.Plugin.Available {
		t.Fatalf("stale registry accepted: available=%v err=%v", state.Plugin.Available, err)
	}
}
