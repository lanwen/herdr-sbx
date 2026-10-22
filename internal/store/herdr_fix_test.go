package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHerdrFixFailureNeedsExplicitRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	require.Equal(t, Severity(SeverityError), SeverityForCode(CodeHerdrFix), "repair failure is not an error")
	require.NoError(t, RecordError(CodeHerdrFix, scope, "handoff unsupported"))
	require.NoError(t, SetRegistrationError(CodeHerdrCompatibility, scope, "forwarding unavailable"))
	require.NoError(t, SetRegistrationError("", scope, ""))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1)
	for _, entry := range status.Errors {
		require.Equal(t, CodeHerdrFix, entry.Code, "wrong remaining failure: %+v", entry)
	}
	require.NoError(t, ClearError(CodeHerdrFix, scope))
	status, err = ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 0)
}
