package registration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRecreatedSandboxResolvesOnlyRetiredGuestFailures(t *testing.T) {
	f, scope, failed := newRetiredSetupDiagnosticFixture(t)
	require.NoError(t, store.RecordError(store.CodeSandboxSetup, failed.Scope, "guest readiness probe timed out"))
	require.NoError(t, store.RecordError(store.CodeSandboxSetup, store.StatusKey(scope, "one"), "current setup failed"))
	require.NoError(t, store.RecordError(store.CodeSandboxSetup, store.StatusKey("other scope", "retired"), "another daemon failed"))
	require.NoError(t, store.RecordError(store.CodeManagement, failed.Scope, "remove failed"))
	require.NoError(t, store.RecordError(store.CodeSandboxSetup, "retired", "unqualified scope"))
	scan := reconciliation{service: f.service, ctx: context.Background(), prerequisites: f.service.Inspect()}
	require.NoError(t, scan.load())
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 4, "retired guest failure remains active or unrelated errors were cleared")
	require.Contains(t, status.Sandboxes, failed.Scope, "retirement must retain the historical sandbox")
	var resolved []store.EventRecord
	for _, event := range status.Events {
		if event.Ref == failed.Ref && event.Resolved {
			resolved = append(resolved, event)
		}
	}
	require.Len(t, resolved, 1)
	require.Equal(t, failed.Detail, resolved[0].Detail)
	require.Equal(t, failed.Scope, resolved[0].Scope)
	require.Equal(t, failed.Code, resolved[0].Code)
	retiredEvents := 0
	for _, event := range status.Events {
		if event.Scope == failed.Scope && event.Code == store.CodeSandboxSetup && event.Resolved {
			retiredEvents++
		}
	}
	require.Equal(t, 2, retiredEvents, "all retired-instance guest failures should become history")
}

func newRetiredSetupDiagnosticFixture(t *testing.T) (*registrationFixture, string, store.ErrorRecord) {
	t.Helper()
	f, scope, machines, _ := newManagementFixture(t, "stopped")
	*machines = []herdr.Machine{{ID: "machine", Target: "demo.sbx", Session: "default", Enabled: boolPointer(true)}}
	require.NoError(t, store.RecordSandboxStatus(scope, sbx.Sandbox{ID: "retired", Name: "demo", Status: "running"}, "connection failed", "deletion unconfirmed", true))
	key := store.StatusKey(scope, "retired")
	require.NoError(t, store.RecordError(store.CodeSandboxSetup, key, "remote Herdr check: sandbox demo does not exist; auto-create is off"))
	failed, found, err := store.PrimaryError(key)
	require.NoError(t, err)
	require.True(t, found)
	return f, scope, failed
}

func TestGuestFailureRetirementNeedsUniqueSameScopeReplacement(t *testing.T) {
	for _, inventory := range []string{
		`[]`,
		`[{"uuid":"one","name":"unrelated","status":"stopped"}]`,
		`[{"uuid":"one","name":"demo","status":"stopped"},{"uuid":"retired","name":"renamed","status":"stopped"}]`,
		`[{"uuid":"one","name":"demo","status":"stopped"},{"uuid":"two","name":"demo","status":"stopped"}]`,
	} {
		t.Run(inventory, func(t *testing.T) {
			f, _, failed := newRetiredSetupDiagnosticFixture(t)
			f.inventory = inventory
			_ = f.service.ReconcileContext(context.Background(), io.Discard)
			assertGuestFailureActive(t, failed)
		})
	}
}

func TestUnreadableInventoryKeepsRetiredGuestFailure(t *testing.T) {
	f, _, failed := newRetiredSetupDiagnosticFixture(t)
	f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("inventory unavailable") })
	f.rebuildClients()
	require.Error(t, f.service.ReconcileContext(context.Background(), io.Discard))
	assertGuestFailureActive(t, failed)
}

func assertGuestFailureActive(t *testing.T, failed store.ErrorRecord) {
	t.Helper()
	status, err := store.ReadStatusStore()
	require.NoError(t, err)
	found := false
	for _, entry := range status.Errors {
		found = found || entry.Ref == failed.Ref
	}
	require.True(t, found, "unproven retirement cleared an active failure")
}
