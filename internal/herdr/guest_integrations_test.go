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

func TestPrepareGuestIntegrations(t *testing.T) {
	for _, tc := range []integrationTestCase{
		{name: "absent configurations"},
		{name: "Claude", claudeDir: ".claude", want: "integration install claude\n"},
		{name: "Codex", codexDir: ".codex", want: "integration install codex\n"},
		{name: "both", claudeDir: ".claude", codexDir: ".codex", want: "integration install claude\nintegration install codex\n"},
		{name: "configuration overrides", claudeDir: "Claude config", codexDir: "Codex config", want: "integration install claude\nintegration install codex\n"},
		{name: "first failure stops preparation", claudeDir: ".claude", codexDir: ".codex", fail: "claude", want: "integration install claude\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testGuestIntegrations(t, tc)
		})
	}
}

type integrationTestCase struct {
	name, claudeDir, codexDir, fail, want string
}

func testGuestIntegrations(t *testing.T, tc integrationTestCase) {
	t.Helper()
	home := t.TempDir()
	env := integrationEnvironment(t, home)
	env = append(env, "FAIL_TARGET="+tc.fail)
	for key, dir := range map[string]string{"CLAUDE_CONFIG_DIR": tc.claudeDir, "CODEX_HOME": tc.codexDir} {
		if dir == "" {
			continue
		}
		require.NoError(t, os.Mkdir(filepath.Join(home, dir), 0700))
		if !strings.HasPrefix(dir, ".") {
			env = append(env, key+"="+filepath.Join(home, dir))
		}
	}
	client := integrationClient(t, env)
	for range 2 {
		err := client.PrepareGuestIntegrations(context.Background(), "test.sbx")
		if tc.fail != "" {
			require.ErrorContains(t, err, "integration rejected")
		} else {
			require.NoError(t, err)
		}
	}
	log, err := os.ReadFile(filepath.Join(home, "calls"))
	require.NoError(t, err)
	require.Equal(t, strings.Repeat(tc.want, 2), string(log))
}

func integrationEnvironment(t *testing.T, home string) []string {
	t.Helper()
	bin := filepath.Join(home, ".local", "bin")
	require.NoError(t, os.MkdirAll(bin, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "herdr"), []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$INTEGRATION_LOG"
if [ "${FAIL_TARGET:-}" = "$3" ]; then
 printf '%s\n' 'integration rejected' >&2
 exit 1
fi
`), 0700))
	log := filepath.Join(home, "calls")
	require.NoError(t, os.WriteFile(log, nil, 0600))
	return []string{"HOME=" + home, "PATH=/usr/bin:/bin", "INTEGRATION_LOG=" + log}
}

func integrationClient(t *testing.T, env []string) *Client {
	t.Helper()
	return New(process.Runner{Execute: func(ctx context.Context, command process.Command) (string, error) {
		require.Equal(t, "ssh", command.Name)
		require.Equal(t, "test.sbx", command.Args[len(command.Args)-2])
		require.Equal(t, 15*time.Second, command.Timeout)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command.Args[len(command.Args)-1])
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		return string(output), err
	}})
}
