package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

const integrationSchema = `{"schemas":{"request":{"$defs":{"IntegrationTarget":{"enum":["claude","codex","antigravity_cli","new-agent","continue"]}}}}}`

func TestPrepareGuestIntegrations(t *testing.T) {
	for _, tc := range []integrationTestCase{
		{name: "no matching kits"},
		{name: "single kit", kits: []string{"codex"}, want: "integration install codex\n"},
		{name: "guest binary fallback", kits: []string{"codex"}, fallback: true, want: "integration install codex\n"},
		{name: "multiple kits", kits: []string{"claude", "codex"}, want: "integration install claude\nintegration install codex\n"},
		{name: "guest target names", kits: []string{"antigravity_cli", "new-agent"}, want: "integration install antigravity_cli\nintegration install new-agent\n"},
		{name: "unknown and differing kit names", kits: []string{"unknown-agent", "my-codex", "claude-mixin"}},
		{name: "executable without kit", executables: []string{"codex"}, want: "integration install codex\n"},
		{name: "multiple executable and kit matches", kits: []string{"claude"}, executables: []string{"codex"}, want: "integration install claude\nintegration install codex\n"},
		{name: "shell builtin is not an executable"},
		{name: "first failure stops preparation", kits: []string{"claude", "codex"}, fail: "claude", want: "integration install claude\n"},
	} {
		t.Run(tc.name, func(t *testing.T) { testGuestIntegrations(t, tc) })
	}
}

type integrationTestCase struct {
	name, fail, want  string
	kits, executables []string
	fallback          bool
}

func testGuestIntegrations(t *testing.T, tc integrationTestCase) {
	t.Helper()
	home := t.TempDir()
	env := integrationEnvironment(t, home)
	env = append(env, "FAIL_TARGET="+tc.fail)
	if tc.fallback {
		env = append(env, "PATH=/usr/bin:/bin")
	}
	for _, kit := range tc.kits {
		require.NoError(t, os.MkdirAll(filepath.Join(home, "kits", kit), 0700))
	}
	for _, executable := range tc.executables {
		require.NoError(t, os.WriteFile(filepath.Join(home, ".local", "bin", executable), []byte("#!/bin/sh\nexit 0\n"), 0700))
	}
	client := integrationClient(t, home, env)
	require.NoError(t, os.WriteFile(filepath.Join(home, "continue"), []byte("#!/bin/sh\nexit 0\n"), 0700))
	for range 2 {
		err := client.PrepareGuestIntegrations(context.Background(), "test.sbx", nil)
		if tc.fail != "" {
			require.ErrorContains(t, err, "integration rejected")
		} else {
			require.NoError(t, err)
		}
	}
	log, err := os.ReadFile(filepath.Join(home, "calls"))
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("api schema --json\n"+tc.want, 2), string(log))
}

func TestPrepareGuestIntegrationsRejectsInvalidDiscovery(t *testing.T) {
	for _, tc := range []struct{ name, schema, fail, want string }{
		{"retrieval failed", integrationSchema, "schema", "schema unavailable"},
		{"malformed JSON", "not JSON", "", "invalid integration target schema"},
		{"missing target definition", `{}`, "", "no integration targets"},
		{"empty targets", `{"schemas":{"request":{"$defs":{"IntegrationTarget":{"enum":[]}}}}}`, "", "no integration targets"},
		{"invalid target type", `{"schemas":{"request":{"$defs":{"IntegrationTarget":{"enum":[1]}}}}}`, "", "invalid integration target schema"},
		{"unsafe later target", strings.Replace(integrationSchema, "codex", "../codex", 1), "", "unsafe integration target"},
		{"option target", strings.Replace(integrationSchema, "codex", "--codex", 1), "", "unsafe integration target"},
		{"shell syntax", strings.Replace(integrationSchema, "codex", "codex;exit", 1), "", "unsafe integration target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			env := integrationEnvironment(t, home)
			env = append(env, "FAIL_TARGET="+tc.fail, "INTEGRATION_SCHEMA="+tc.schema)
			require.NoError(t, os.MkdirAll(filepath.Join(home, "kits", "claude"), 0700))
			err := integrationClient(t, home, env).PrepareGuestIntegrations(context.Background(), "test.sbx", nil)
			require.ErrorContains(t, err, tc.want)
			log, readErr := os.ReadFile(filepath.Join(home, "calls"))
			require.NoError(t, readErr)
			require.Equal(t, "api schema --json\n", string(log))
		})
	}
}

func integrationEnvironment(t *testing.T, home string) []string {
	t.Helper()
	bin := filepath.Join(home, ".local", "bin")
	require.NoError(t, os.MkdirAll(bin, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "herdr"), []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$INTEGRATION_LOG"
if [ "$1 $2" = 'api schema' ]; then
 if [ "${FAIL_TARGET:-}" = schema ]; then
  printf '%s\n' 'schema unavailable' >&2
  exit 1
 fi
 printf '%s\n' "$INTEGRATION_SCHEMA"
elif [ "${FAIL_TARGET:-}" = "$3" ]; then
 printf '%s\n' 'integration rejected' >&2
 exit 1
fi
`), 0700))
	log := filepath.Join(home, "calls")
	require.NoError(t, os.WriteFile(log, nil, 0600))
	return []string{"HOME=" + home, "PATH=" + bin + ":/usr/bin:/bin", "INTEGRATION_LOG=" + log, "INTEGRATION_SCHEMA=" + integrationSchema}
}

func integrationClient(t *testing.T, home string, env []string) *Client {
	t.Helper()
	return New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		require.Equal(t, "ssh", command.Name)
		require.Equal(t, "test.sbx", command.Args[len(command.Args)-2])
		require.Equal(t, 15*time.Second, command.Timeout)
		script := strings.ReplaceAll(command.Args[len(command.Args)-1], "/usr/share/sandbox/kit/", filepath.Join(home, "kits")+"/")
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		cmd.Env = env
		cmd.Dir = home
		output, err := cmd.CombinedOutput()
		return string(output), err
	}})
}
