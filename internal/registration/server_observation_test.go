package registration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGuestServerObservationUsesDurableAuditAndRetainsLastVersion(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	probes := 0
	fail := false
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		require.EqualValues(t, "ssh", command.Name)
		require.Contains(t, strings.Join(command.Args, " "), "status server --json")
		probes++
		if fail {
			return "", errors.New("connection refused")
		}
		return `{"running":true,"version":"0.9.2"}`, nil
	}
	f.rebuildClients()
	sb := sbx.Sandbox{ID: "stable", Name: "demo", Status: "running"}
	current := func() bool { return true }
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	for range 3 {
		f.service.observeGuestServer(context.Background(), "scope", sb, current)
	}
	require.Equal(t, 1, probes, "catch-ups repeated probe: %d", probes)
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	seen := status.Sandboxes["scope/stable"].GuestServer
	require.NotNil(t, seen)
	require.EqualValues(t, "0.9.2", seen.Version)
	require.True(t, seen.Current)
	require.False(t, seen.ObservedAt.IsZero())
	// Passive inventory/status reads preserve the observation and never probe.
	for range 3 {
		{
			_, err := store.Snapshot("scope")
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, probes, "poll opened SSH")
	require.NoError(t, store.UpdateStatusStore(func(s *store.StatusStore) {
		s.Sandboxes["scope/stable"].GuestServer.CheckedAt = time.Now().Add(-16 * time.Minute)
	}))
	fail = true
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	last := status.Sandboxes["scope/stable"].GuestServer
	require.EqualValues(t, 2, probes)
	require.False(t, last.Current)
	require.False(t, last.Version != seen.Version)
	require.True(t, last.ObservedAt.Equal(seen.ObservedAt))
	require.Len(t, status.Errors, 0)
	fail = false
	for _, ctx := range []context.Context{WithFreshGuestObservation(context.Background()), context.WithValue(context.Background(), guestPluginRetryKey{}, true)} {
		f.service.observeGuestServer(ctx, "scope", sb, current)
	}
	require.Equal(t, 4, probes, "fresh/re-sync did not refresh: %d", probes)
	sb.Status = "stopped"
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	require.NoError(t, store.ObserveSandbox("scope", sb))
	status, err = store.ReadStatusStore()
	require.NoError(t, err)
	require.EqualValues(t, 4, probes)
	require.False(t, status.Sandboxes["scope/stable"].GuestServer.Current)
	// A new stable identity with the same name cannot inherit its version.
	sb.ID, sb.Status = "replacement", "running"
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	require.Equal(t, 5, probes, "recreated sandbox inherited stale cache")
}
