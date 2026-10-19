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
)

func TestGuestServerObservationUsesDurableAuditAndRetainsLastVersion(t *testing.T) {
	f := newRegistrationFixture(t, "[]")
	probes := 0
	fail := false
	f.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name != "ssh" || !strings.Contains(strings.Join(command.Args, " "), "status server --json") {
			t.Fatalf("unexpected command: %+v", command)
		}
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
	if probes != 1 {
		t.Fatalf("catch-ups repeated probe: %d", probes)
	}
	status, err := store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	seen := status.Sandboxes["scope/stable"].GuestServer
	if seen == nil || seen.Version != "0.9.2" || !seen.Current || seen.ObservedAt.IsZero() {
		t.Fatalf("observation: %+v", seen)
	}
	// Passive inventory/status reads preserve the observation and never probe.
	for range 3 {
		if _, err := store.Snapshot("scope"); err != nil {
			t.Fatal(err)
		}
	}
	if probes != 1 {
		t.Fatal("poll opened SSH")
	}
	if err := store.UpdateStatusStore(func(s *store.StatusStore) {
		s.Sandboxes["scope/stable"].GuestServer.CheckedAt = time.Now().Add(-16 * time.Minute)
	}); err != nil {
		t.Fatal(err)
	}
	fail = true
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	status, err = store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	last := status.Sandboxes["scope/stable"].GuestServer
	if probes != 2 || last.Current || last.Version != seen.Version || !last.ObservedAt.Equal(seen.ObservedAt) || len(status.Errors) != 0 {
		t.Fatalf("failure erased version, repeated probe, or blocked work: probes=%d observation=%+v", probes, last)
	}
	fail = false
	for _, ctx := range []context.Context{WithFreshGuestObservation(context.Background()), context.WithValue(context.Background(), guestPluginRetryKey{}, true)} {
		f.service.observeGuestServer(ctx, "scope", sb, current)
	}
	if probes != 4 {
		t.Fatalf("fresh/re-sync did not refresh: %d", probes)
	}
	sb.Status = "stopped"
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	if err := store.ObserveSandbox("scope", sb); err != nil {
		t.Fatal(err)
	}
	status, err = store.ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if probes != 4 || status.Sandboxes["scope/stable"].GuestServer.Current {
		t.Fatal("stopped sandbox probed or marked current")
	}
	// A new stable identity with the same name cannot inherit its version.
	sb.ID, sb.Status = "replacement", "running"
	f.service.observeGuestServer(context.Background(), "scope", sb, current)
	if probes != 5 {
		t.Fatal("recreated sandbox inherited stale cache")
	}
}
