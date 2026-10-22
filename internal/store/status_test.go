package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/stretchr/testify/require"
)

func TestPreparationCooldownIsSharedThroughStatusStore(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	key := StatusKey("scope", "old")
	require.NoError(t, NotePreparationFailure(key, "distinctive failure", time.Minute))
	{
		allowed, err := PreparationAllowed(key)
		require.NoError(t, err)
		require.False(t, allowed)
	}
	{
		detail, err := PreparationCooldownDetail(key)
		require.NoError(t, err)
		require.Contains(t, detail, "distinctive failure")
	}
	require.NoError(t, ClearPreparationFailure(key))
	{
		allowed, err := PreparationAllowed(key)
		require.NoError(t, err)
		require.True(t, allowed)
	}
}

func TestPolicyAttemptRetainsOriginalScope(t *testing.T) {
	for _, selector := range []string{"sandbox-id", "demo"} {
		attempt := PolicyAttempt{PolicySandbox: selector, Scope: "scope", Sandbox: sbx.Sandbox{ID: "old", Name: "demo"}, Intent: &sbx.PolicyAccess{Host: "herdr.dev:443", Path: "/latest.json"}}
		encoded, err := json.Marshal(attempt)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"host":`)
		require.Contains(t, string(encoded), `"path":`)
		var restored PolicyAttempt
		require.NoError(t, json.Unmarshal(encoded, &restored))
		require.Equal(t, selector, restored.PolicySandbox, "restored selector=%q, want %q", restored.PolicySandbox, selector)
	}
}
