package registration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestInlineEnableKeepsRegistrationEvidenceOffProcessOutput(t *testing.T) {
	if mode := os.Getenv("HERDR_SBX_OUTPUT_TEST_HELPER"); mode != "" {
		runInlineEnableOutputHelper(t, mode)
		return
	}
	for _, mode := range []string{"success", "evidence save failure"} {
		t.Run(mode, func(t *testing.T) {
			evidencePath := filepath.Join(t.TempDir(), "events.json")
			command := exec.Command(os.Args[0], "-test.run=^TestInlineEnableKeepsRegistrationEvidenceOffProcessOutput$")
			command.Env = append(os.Environ(), "HERDR_SBX_OUTPUT_TEST_HELPER="+mode, "HERDR_SBX_OUTPUT_TEST_EVENTS="+evidencePath)
			var stdout, stderr strings.Builder
			command.Stdout, command.Stderr = &stdout, &stderr
			require.NoError(t, command.Run(), "helper failed: stdout=%s stderr=%s", stdout.String(), stderr.String())
			require.Equal(t, "PASS\n", stdout.String(), "inline Enable wrote outside the panel")
			require.Empty(t, stderr.String(), "registration logger leaked into the terminal")
			data, err := os.ReadFile(evidencePath)
			require.NoError(t, err)
			var events []store.EventRecord
			require.NoError(t, json.Unmarshal(data, &events))
			var evidence strings.Builder
			for _, event := range events {
				evidence.WriteString(event.Detail + "\n" + event.Output + "\n")
			}
			require.Contains(t, evidence.String(), "machine_add")
			require.Contains(t, evidence.String(), "machine-1")
			require.Contains(t, evidence.String(), `"stage":"machine_add"`)
			require.Contains(t, evidence.String(), `"attempt":1`)
			require.Contains(t, evidence.String(), `"deadline":`)
		})
	}
}

func runInlineEnableOutputHelper(t *testing.T, mode string) {
	t.Helper()
	fixture, _ := newAddVerificationFixture(t, "delayed")
	scope := store.ScopeID("/tmp/test-sandboxd.sock")
	require.NoError(t, fixture.service.SetEnabled(context.Background(), scope, "sandbox-1", store.Enabled))
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	data, err := json.Marshal(status.Events)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("HERDR_SBX_OUTPUT_TEST_EVENTS"), data, 0600))
	if mode == "evidence save failure" {
		path, err := store.StatePath()
		require.NoError(t, err)
		require.NoError(t, os.Remove(path+".status"))
		require.NoError(t, os.Mkdir(path+".status", 0700))
		fixture.service.logRegistration(context.Background(), registrationLog{Stage: "machine_add", Scope: scope, Sandbox: sbx.Sandbox{ID: "sandbox-1", Name: "demo"}, Started: time.Now(), Timeout: time.Second, Err: errors.New("record could not be saved")})
	}
}
