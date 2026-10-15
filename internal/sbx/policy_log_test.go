package sbx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestBlockedNetworkRequests(t *testing.T) {
	t.Setenv("HERDR_SANDBOXES_SBX_APP_NAME", "")
	client := New(process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		want := "policy log workbox --json --type network --limit 100"
		if strings.Join(command.Args, " ") != want {
			t.Fatalf("args=%q, want %q", command.Args, want)
		}
		return `{"updated_at":"2026-10-15T12:00:00Z","allowed":[],"blocked":[{"sandbox_id":"sandbox-id","host":"example.org","reason":"default-deny","count":2,"last_seen":"2026-10-15T11:59:59Z"}]}`, nil
	}})
	entries, err := client.BlockedNetworkRequests(context.Background(), "workbox")
	if err != nil || len(entries) != 1 || entries[0].Host != "example.org" || entries[0].Count != 2 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
}

func TestBlockedNetworkRequestsRejectsUnknownLog(t *testing.T) {
	client := New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return `{"allowed":[]}`, nil
	}})
	if _, err := client.BlockedNetworkRequests(context.Background(), "workbox"); err == nil {
		t.Fatal("missing blocked list was accepted as evidence of no policy block")
	}
	client = New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		return "offline", errors.New("unavailable")
	}})
	if _, err := client.BlockedNetworkRequests(context.Background(), "workbox"); err == nil {
		t.Fatal("unavailable log was accepted")
	}
}
