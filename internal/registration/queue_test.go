package registration

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestExplicitResyncWaitsForCurrentRegistration(t *testing.T) {
	fixture, scope, machines, calls := newManagementFixture(t, "running")
	*machines = []herdr.Machine{{ID: "machine", Label: "Desk", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	unlock, err := store.LockState()
	require.NoError(t, err)
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- fixture.service.RetrySelected(ctx, io.Discard, SandboxSelection{Scope: scope, Socket: "/tmp/test-sandboxd.sock", ID: "one", Name: "demo"})
	}()
	select {
	case err := <-done:
		t.Fatalf("explicit request was dropped instead of queued: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("queued Re-sync did not resume")
	}
	require.Contains(t, strings.Join(*calls, "\n"), "demo.sbx", "queued Re-sync never reached its sandbox work")
}

func TestQueuedExplicitResyncCanBeCancelled(t *testing.T) {
	fixture, scope, _, _ := newManagementFixture(t, "running")
	unlock, err := store.LockState()
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- fixture.service.RetrySelected(ctx, io.Discard, SandboxSelection{Scope: scope, Socket: "/tmp/test-sandboxd.sock", ID: "one", Name: "demo"})
	}()
	cancel()
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled), "unexpected cancellation: %v", err)
	case <-time.After(time.Second):
		t.Fatal("queued Re-sync ignored cancellation")
	}
}
