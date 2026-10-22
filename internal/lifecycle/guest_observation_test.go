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
	for _, trigger := range []string{"periodic", "started", "reconnect", "sequence gap"} {
		t.Run(trigger, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.request = func(r *http.Request) (*http.Response, error) {
				return lifecycleResponse(200, `[{"id":"old","name":"demo","status":"running"}]`), nil
			}
			item := ownedRegistration()
			fingerprint, err := guestplugins.DesiredFingerprint(context.Background(), f.service.herdr, f.scope, item.SandboxID)
			require.NoError(t, err)
			checked := time.Now().Add(-time.Minute)
			item.GuestSync = &store.GuestSyncObservation{Fingerprint: item.Target + "\x00" + item.SandboxID + "\x00" + fingerprint, CheckedAt: checked, Status: "ready", Detail: "saved observation"}
			f.save(item)
			scheduler := f.scheduler()
			scheduler.beginStream()
			switch trigger {
			case "periodic":
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
			saved, err := store.LoadState()
			require.NoError(t, err)
			got := saved.Scopes[f.scope][0].GuestSync.CheckedAt
			require.False(t, trigger == "periodic" && !got.Equal(checked), "ordinary catch-up repeated guest observation")
			require.False(t, trigger != "periodic" && !got.After(checked), "%s did not refresh saved observation", trigger)
		})
	}
}
