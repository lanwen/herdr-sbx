package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func TestPreparationCooldownIsSharedThroughStatusStore(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	key := StatusKey("scope", "old")
	if err := NotePreparationFailure(key, "distinctive failure", time.Minute); err != nil {
		t.Fatal(err)
	}
	if allowed, err := PreparationAllowed(key); err != nil || allowed {
		t.Fatal("cooldown was not persisted")
	}
	if detail, err := PreparationCooldownDetail(key); err != nil || !strings.Contains(detail, "distinctive failure") {
		t.Fatalf("failure detail=%q, %v", detail, err)
	}
	if err := ClearPreparationFailure(key); err != nil {
		t.Fatal(err)
	}
	if allowed, err := PreparationAllowed(key); err != nil || !allowed {
		t.Fatal("explicit retry did not clear persisted cooldown")
	}
}

func TestPolicyAttemptRetainsOriginalScope(t *testing.T) {
	for _, selector := range []string{"", "demo"} {
		attempt := PolicyAttempt{PolicySandbox: selector, Scope: "scope", Sandbox: sbx.Sandbox{ID: "old", Name: "demo"}, Intent: &sbx.PolicyAccess{Host: "herdr.dev:443", Path: "/latest.json"}}
		encoded, err := json.Marshal(attempt)
		if err != nil || !strings.Contains(string(encoded), `"host":`) || !strings.Contains(string(encoded), `"path":`) {
			t.Fatalf("journal schema=%s, %v", encoded, err)
		}
		var restored PolicyAttempt
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		want := selector
		if want == "" {
			want = "old"
		}
		if restored.PolicySandboxName() != want {
			t.Fatalf("restored selector=%q, want %q", restored.PolicySandboxName(), want)
		}
	}
}
