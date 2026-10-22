package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGuestPluginAttemptRequiresExplicitRetryForSameRevision(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	const scope, sandbox, plugin = "scope", "sandbox", "example.plugin"
	allowed, err := BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", false)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, FinishGuestPluginAttempt(scope, sandbox, plugin, "revision-a", "build failed"))
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", false)
	require.NoError(t, err)
	require.False(t, allowed)
	attempt, found, err := GuestPluginAttemptFor(scope, sandbox, plugin, "revision-a")
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, "build failed", attempt.Detail)
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-b", false)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = BeginGuestPluginAttempt(scope, sandbox, plugin, "revision-a", true)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, FinishGuestPluginAttempt(scope, sandbox, plugin, "revision-a", ""))
	_, found, err = GuestPluginAttemptFor(scope, sandbox, plugin, "revision-a")
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = GuestPluginAttemptFor(scope, sandbox, plugin, "revision-b")
	require.NoError(t, err)
	require.True(t, found)
}
