package registration

import (
	"context"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestFreshPendingObservationDoesNotRepeatManualCatalogAdvice(t *testing.T) {
	for _, savedWarning := range []bool{false, true} {
		name := "no saved cause"
		if savedWarning {
			name = "saved generic warning"
		}
		t.Run(name, func(t *testing.T) {
			fixture, scope, _, _ := newManagementFixture(t, "running")
			pending := store.Tracked{SandboxID: "one", Target: "demo.sbx", Label: "sbx · demo", Pending: true}
			require.NoError(t, store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {pending}}}))
			if savedWarning {
				require.NoError(t, store.SetRegistrationError(store.CodeRegistration, store.StatusKey(scope, "one"), "Herdr entry creation is not confirmed. Check the herdr machine catalog. Retry Enable only after it confirms no matching entry."))
			}
			rows, err := fixture.service.SandboxRows(context.Background(), "/tmp/test-sandboxd.sock")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.False(t, rows[0].EntryPresent)
			t.Logf("fresh pending detail: %s", rows[0].Status.Detail)
			require.NotContains(t, rows[0].Status.Detail, "Check the herdr machine catalog", "fresh absence must lead to plugin-managed recovery instead of asking the user to repeat its own catalog check")
		})
	}
}
