package registration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRegistrationRetainsOriginalCommandEvidence(t *testing.T) {
	fixture, _ := newAddVerificationFixture(t, "absent")
	original := fixture.runner.Execute
	output := "failure prefix " + strings.Repeat("evidence ", 30) + "\x1b[31moriginal-platform-cause-tail\x1b[0m"
	fixture.runner.Execute = func(ctx context.Context, cmd process.Command) (string, error) {
		if cmd.Name == "herdr" && len(cmd.Args) > 1 && cmd.Args[0] == "machine" && cmd.Args[1] == "add" {
			return output, errors.New("exit status 1")
		}
		return original(ctx, cmd)
	}
	fixture.rebuildClients()
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	var preserved string
	for _, event := range status.Events {
		if strings.Contains(event.Output, "original-platform-cause-tail") {
			preserved = event.Output
		}
	}
	require.Contains(t, preserved, "original-platform-cause-tail", "original evidence must survive beyond abbreviated UI detail")
	require.NotContains(t, preserved, "\x1b[")
}

func TestTransientPlatformFailureRetriesOnceAfterFreshChecks(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "absent")
	original := fixture.runner.Execute
	fixture.runner.Execute = func(ctx context.Context, cmd process.Command) (string, error) {
		call := cmd.Name + " " + strings.Join(cmd.Args, " ")
		if call == "herdr machine add --label sbx · demo demo.sbx" && calls.adds == 0 {
			calls.adds++
			return "remote platform detection failed: sandbox is unavailable", errors.New("exit status 1")
		}
		if call == "herdr machine list --json" && calls.adds >= 2 {
			return verifiedCatalog(), nil
		}
		return original(ctx, cmd)
	}
	fixture.rebuildClients()
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	require.Equal(t, 2, calls.adds, "transient unavailable should recover once after readiness, identity and absence checks")
	state, err := store.LoadState()
	require.NoError(t, err)
	require.Equal(t, "machine-1", state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")][0].ProfileID)
}

func TestRegistrationEvidenceRedactsSecretsInErrorAndOutput(t *testing.T) {
	fixture := newRegistrationFixture(t, `[]`)
	var logs strings.Builder
	fixture.service.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	secret := "private-registration-test-secret"
	fixture.service.logRegistration(context.Background(), registrationLog{Stage: "machine_add", Scope: "scope", Sandbox: sbx.Sandbox{ID: "one", Name: "demo"}, Attempt: 1, Started: time.Now(), Timeout: time.Second, Err: errors.New("Authorization: Bearer " + secret), Output: "token=" + secret})
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.NotContains(t, logs.String(), secret)
	require.Len(t, status.Events, 1)
	require.NotContains(t, status.Events[0].Detail+status.Events[0].Output, secret)
	require.Contains(t, status.Events[0].Output, "<REDACTED>")
}
