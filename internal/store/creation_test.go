package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreationHandoffOrdersExplicitCancellation(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel first", false: "handoff first"}[cancelFirst], func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			accepted, err := RequestCreation("scope", "sandbox", "demo")
			require.NoError(t, err)
			require.True(t, accepted)
			if cancelFirst {
				require.NoError(t, SetEnabled("scope", "sandbox", Disabled))
			}
			allowed, err := HandoffCreationRequest("scope", "sandbox", "demo", true)
			require.NoError(t, err)
			require.Equal(t, !cancelFirst, allowed)
			if !cancelFirst {
				require.NoError(t, SetEnabled("scope", "sandbox", Disabled))
			}
			accepted, err = RequestCreation("scope", "sandbox", "demo")
			require.NoError(t, err)
			require.False(t, accepted, "a duplicate event must not revive cancelled permission")
			saved, err := ReadStatusStore()
			require.NoError(t, err)
			require.Empty(t, saved.CreationRequests)
			require.Equal(t, Disabled, saved.EnabledChoices[StatusKey("scope", "sandbox")])
		})
	}
}

func TestDeletedCreationRequestCannotAuthorizeAdd(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	accepted, err := RequestCreation("scope", "sandbox", "demo")
	require.NoError(t, err)
	require.True(t, accepted)
	require.NoError(t, ClearCreationRequest("scope", "sandbox"))
	allowed, err := HandoffCreationRequest("scope", "sandbox", "demo", true)
	require.NoError(t, err)
	require.False(t, allowed, "a deleted request cannot authorize a creation add")
}

func TestOrdinaryRegistrationDoesNotRequireCreationRequest(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	allowed, err := HandoffCreationRequest("scope", "sandbox", "demo", false)
	require.NoError(t, err)
	require.True(t, allowed)
}
