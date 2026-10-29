package lifecycle

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGuestObservationRefreshesAfterLifecycleChanges(t *testing.T) {
	for _, trigger := range []string{"periodic", "started", "reconnect", "sequence gap", "upgrade"} {
		t.Run(trigger, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.request = func(r *http.Request) (*http.Response, error) {
				return lifecycleResponse(200, `[{"id":"old","name":"demo","status":"running"}]`), nil
			}
			checked := time.Now().Add(-time.Minute)
			seedGuestObservation(t, f, trigger, checked)

			scheduler := f.scheduler()
			scheduler.beginStream()
			refreshGuestObservation(scheduler, trigger)

			saved, err := store.LoadState()
			require.NoError(t, err)
			got := saved.Scopes[f.scope][0].GuestSync.CheckedAt
			require.False(t, trigger == "periodic" && !got.Equal(checked), "ordinary catch-up repeated guest observation")
			require.False(t, trigger != "periodic" && !got.After(checked), "%s did not refresh saved observation", trigger)
			if trigger == "upgrade" {
				scheduler.processCatchup(true)
				repeated, err := store.LoadState()
				require.NoError(t, err)
				require.True(t, repeated.Scopes[f.scope][0].GuestSync.CheckedAt.Equal(got), "upgraded setup was repeated on ordinary catch-up")
			}
		})
	}
}

func seedGuestObservation(t *testing.T, f *lifecycleFixture, trigger string, checked time.Time) {
	t.Helper()
	item := ownedRegistration()
	f.save(item)
	if trigger == "upgrade" {
		fingerprint, err := guestplugins.DesiredFingerprint(context.Background(), f.service.herdr, f.scope, item.SandboxID)
		require.NoError(t, err)
		item.GuestSync = &store.GuestSyncObservation{Fingerprint: item.Target + "\x00" + item.SandboxID + "\x00" + fingerprint, Status: "ready", Detail: "previous setup revision"}
	} else {
		f.scheduler().processCatchup(true)
		prepared, err := store.LoadState()
		require.NoError(t, err)
		item = prepared.Scopes[f.scope][0]
		require.NotNil(t, item.GuestSync)
	}
	item.GuestSync.CheckedAt = checked
	f.save(item)

}

func refreshGuestObservation(scheduler *lifecycleScheduler, trigger string) {
	switch trigger {
	case "periodic", "upgrade":
		scheduler.processCatchup(true)
	case "started":
		scheduler.handle(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "started", SandboxID: "old", SandboxName: "demo"})
	case "sequence gap":
		for _, seq := range []uint64{1, 3} {
			scheduler.enqueue(sbx.LifecycleEvent{Type: "sandbox.lifecycle", Action: "stopped", SandboxID: "old", Seq: &seq})
		}
		scheduler.processCatchup(false)
	case "reconnect":
		scheduler.beginStream()
		scheduler.processCatchup(false)
	}
}
