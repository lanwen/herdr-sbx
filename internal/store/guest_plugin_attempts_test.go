package store

import (
	"testing"
)

func TestGuestPluginAttemptRequiresExplicitRetryForSameRevision(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	const scope, sandbox, plugin = "scope", "sandbox", "example.plugin"
	allowed, err := BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", false)
	if err != nil || !allowed {
		t.Fatalf("first attempt: allowed=%t err=%v", allowed, err)
	}
	if err := FinishGuestPluginAttempt(scope, sandbox, plugin, "revision-a", "build failed"); err != nil {
		t.Fatal(err)
	}
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", false)
	if err != nil || allowed {
		t.Fatalf("automatic retry: allowed=%t err=%v", allowed, err)
	}
	attempt, found, err := GuestPluginAttemptFor(scope, sandbox, plugin, "revision-a")
	if err != nil || !found || attempt.Detail != "build failed" {
		t.Fatalf("durable failure: attempt=%+v found=%t err=%v", attempt, found, err)
	}
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-b", false)
	if err != nil || !allowed {
		t.Fatalf("new revision: allowed=%t err=%v", allowed, err)
	}
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", true)
	if err != nil || !allowed {
		t.Fatalf("explicit retry: allowed=%t err=%v", allowed, err)
	}
	if err := FinishGuestPluginAttempt(scope, sandbox, plugin, "revision-a", ""); err != nil {
		t.Fatal(err)
	}
	_, found, err = GuestPluginAttemptFor(scope, sandbox, plugin, "revision-a")
	if err != nil || found {
		t.Fatalf("confirmed recovery retained attempt: found=%t err=%v", found, err)
	}
	_, found, err = GuestPluginAttemptFor(scope, sandbox, plugin, "revision-b")
	if err != nil || !found {
		t.Fatalf("cleared another revision: found=%t err=%v", found, err)
	}
}
