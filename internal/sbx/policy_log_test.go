package sbx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestBlockedNetworkRequests(t *testing.T) {
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		want := "policy log workbox --json --type network --limit 100"
		require.Equal(t, want, strings.Join(command.Args, " "), "args=%q, want %q", command.Args, want)
		return `{"updated_at":"2026-10-15T12:00:00Z","allowed":[],"blocked":[{"sandbox_id":"sandbox-id","host":"example.org","reason":"default-deny","count":2,"last_seen":"2026-10-15T11:59:59Z"}]}`, nil
	}})
	entries, err := client.BlockedNetworkRequests(context.Background(), "workbox")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.EqualValues(t, "example.org", entries[0].Host)
	require.EqualValues(t, 2, entries[0].Count)
}

func TestBlockedNetworkRequestsRejectsUnknownLog(t *testing.T) {
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return `{"allowed":[]}`, nil
	}})
	{
		_, err := client.BlockedNetworkRequests(context.Background(), "workbox")
		require.Error(t, err, "missing blocked list was accepted as evidence of no policy block")
	}
	client = New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return "offline", errors.New("unavailable")
	}})
	{
		_, err := client.BlockedNetworkRequests(context.Background(), "workbox")
		require.Error(t, err, "unavailable log was accepted")
	}
}
