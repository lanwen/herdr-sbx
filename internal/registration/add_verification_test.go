package registration

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMachineAddChecksCatalogThreeTimes(t *testing.T) {
	for _, mode := range []string{"delayed", "absent", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			fixture, calls := newAddVerificationFixture(t, mode)
			var output strings.Builder
			require.NoError(t, fixture.service.ReconcileContext(context.Background(), &output))
			require.Equal(t, 3, calls.checks)
			require.Equal(t, 1, calls.adds)
			state, err := store.LoadState()
			require.NoError(t, err)
			item := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")][0]
			if mode == "delayed" {
				require.Equal(t, "machine-1", item.ProfileID)
				require.False(t, item.Pending)
			} else {
				require.True(t, item.Pending)
				if mode == "unavailable" {
					require.Contains(t, output.String(), "could not check whether Herdr saved the connection")
					require.Contains(t, output.String(), "check again automatically")
				} else {
					require.Contains(t, output.String(), "3 checks")
				}
			}
		})
	}
}

func TestPendingMachineAddDoesNotReplayAutomatically(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "absent")
	for range 2 {
		require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	}
	require.Equal(t, 1, calls.adds, "catalog absence caused an automatic second machine add")
}

type addVerificationCalls struct {
	adds, checks, native int
	onCheck              func()
}

func newAddVerificationFixture(t *testing.T, mode string) (*registrationFixture, *addVerificationCalls) {
	t.Helper()
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	calls := &addVerificationCalls{}
	ordinary := &pendingRegistrationCommands{t: t}
	fixture.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		if strings.HasPrefix(call, "herdr machine enable ") || strings.HasPrefix(call, "herdr machine disable ") {
			calls.native++
			return "", errors.New("unexpected native mutation")
		}
		switch call {
		case "herdr machine add --label sbx · demo demo.sbx":
			calls.adds++
			if mode == "untrusted" {
				return "connection lost", errors.New("connection lost")
			}
			return "Saved SSH machine machine-1. Remote server is ready.\n", nil
		case "herdr machine list --json":
			if calls.adds == 0 {
				return `[]`, nil
			}
			calls.checks++
			if calls.onCheck != nil {
				calls.onCheck()
			}
			return calls.catalog(mode)

		default:
			return ordinary.execute(ctx, command)
		}
	}
	fixture.rebuildClients()
	return fixture, calls
}

func (calls *addVerificationCalls) catalog(mode string) (string, error) {
	switch mode {
	case "unavailable":
		return "catalog unavailable", errors.New("connection refused")
	case "mixed absent":
		if calls.checks == 1 {
			return "catalog unavailable", errors.New("connection refused")
		}
	case "mixed unavailable":
		if calls.checks == 3 {
			return "catalog unavailable", errors.New("connection refused")
		}
	case "delayed":
		if calls.checks >= 3 {
			return verifiedCatalog(), nil
		}
	case "later":
		if calls.checks > 3 {
			return verifiedCatalog(), nil
		}
	case "untrusted", "wrong id":
		return `[{"id":"unrelated","label":"sbx · demo","target":"demo.sbx","session":"default","enabled":false}]`, nil
	case "label collision":
		return `[{"id":"unrelated","label":"sbx · demo","target":"other.sbx","session":"default"}]`, nil
	case "wrong target":
		return `[{"id":"machine-1","label":"sbx · demo","target":"other.sbx","session":"default"}]`, nil
	case "wrong session":
		return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"other"}]`, nil
	}
	return `[]`, nil
}

func verifiedCatalog() string {
	return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default","enabled":true}]`
}

func TestPendingCandidatesCannotBecomeOwnedWithoutTrustedMatch(t *testing.T) {
	for _, mode := range []string{"untrusted", "wrong id", "wrong target", "wrong session"} {
		t.Run(mode, func(t *testing.T) {
			fixture, calls := newAddVerificationFixture(t, mode)
			for range 2 {
				require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
			}
			scope := store.ScopeID("/tmp/test-sandboxd.sock")
			require.Error(t, fixture.service.SetEnabled(context.Background(), scope, "sandbox-1", store.Enabled))
			state, err := store.LoadState()
			require.NoError(t, err)
			require.Empty(t, state.Scopes[scope][0].ProfileID)
			require.True(t, state.Scopes[scope][0].Pending)
			require.Equal(t, 1, calls.adds)
			require.Zero(t, calls.native)
		})
	}
}

func TestPendingTrustedIDCanBeConfirmedOnLaterCatalogRead(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "later")
	for range 2 {
		require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	}
	state, err := store.LoadState()
	require.NoError(t, err)
	item := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")][0]
	require.Equal(t, "machine-1", item.ProfileID)
	require.Empty(t, item.PendingProfileID)
	require.False(t, item.Pending)
	require.Equal(t, 1, calls.adds)
}

func TestAddVerificationUsesFinalCatalogOutcome(t *testing.T) {
	for _, mode := range []string{"mixed absent", "mixed unavailable"} {
		t.Run(mode, func(t *testing.T) {
			fixture, _ := newAddVerificationFixture(t, mode)
			var output strings.Builder
			require.NoError(t, fixture.service.ReconcileContext(context.Background(), &output))
			if mode == "mixed absent" {
				require.Contains(t, output.String(), "no connection was saved")
			} else {
				require.Contains(t, output.String(), "could not check")
			}
		})
	}
}

func TestAddVerificationCancellationRetainsAcknowledgement(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "absent")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls.onCheck = cancel
	require.NoError(t, fixture.service.ReconcileContext(ctx, io.Discard))
	require.Equal(t, 1, calls.checks)
	state, err := store.LoadState()
	require.NoError(t, err)
	item := state.Scopes[store.ScopeID("/tmp/test-sandboxd.sock")][0]
	require.True(t, item.Pending)
	require.Equal(t, "machine-1", item.PendingProfileID)
}

func TestLaterCatalogAbsenceUpdatesActivePendingOutcome(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "unavailable")
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	original := fixture.runner.Execute
	fixture.runner.Execute = func(ctx context.Context, cmd process.Command) (string, error) {
		if cmd.Name == "herdr" && strings.Join(cmd.Args, " ") == "machine list --json" {
			return `[]`, nil
		}
		return original(ctx, cmd)
	}
	fixture.rebuildClients()
	require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	detail := pendingRegistrationDetail(status, store.StatusKey(store.ScopeID("/tmp/test-sandboxd.sock"), "sandbox-1"))
	require.Contains(t, detail, "no saved connection")
	require.NotContains(t, detail, "could not check")
	require.Equal(t, 1, calls.adds)
}

func TestPendingLabelCollisionIsNeverReportedAsCatalogAbsence(t *testing.T) {
	fixture, calls := newAddVerificationFixture(t, "label collision")
	for range 2 {
		require.NoError(t, fixture.service.ReconcileContext(context.Background(), io.Discard))
	}
	rows, err := fixture.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
	require.NoError(t, err)
	require.Contains(t, rows[0].AssociationIssue, "uses this sandbox's name or address")
	require.NotContains(t, rows[0].Status.Detail, "no matching entry")
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	detail := pendingRegistrationDetail(status, store.StatusKey(store.ScopeID("/tmp/test-sandboxd.sock"), "sandbox-1"))
	require.Contains(t, detail, "uses this sandbox's name or address")
	require.NotContains(t, detail, "no matching")
	require.Equal(t, 1, calls.adds)
}
