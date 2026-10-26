package store

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestErrorReferenceSurvivesRepeatDismissalAndChangesAfterRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, RecordError(CodeManagement, "sandbox", "cannot update"))
	first := mustPrimaryError(t, "sandbox")
	require.True(t, regexp.MustCompile(`^[a-z0-9]{6}$`).MatchString(first.Ref), "invalid reference %q", first.Ref)
	require.NoError(t, DismissError(first))
	require.NoError(t, RecordError(CodeManagement, "sandbox", "cannot update"))
	repeated := mustPrimaryError(t, "sandbox")
	require.False(t, repeated.Ref != first.Ref)
	require.True(t, repeated.Dismissed)
	require.EqualValues(t, 2, repeated.Count)
	require.NoError(t, ClearError(CodeManagement, "sandbox"))

	_, found, err := PrimaryError("sandbox")
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, RecordError(CodeManagement, "sandbox", "cannot update"))

	current := mustPrimaryError(t, "sandbox")
	require.False(t, current.Ref == first.Ref)
	require.False(t, current.Dismissed)
}

func TestPluginReferenceCorrelatesFailureRecoveryAndChangedCause(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.plugin", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", OccurredAt: time.Now()}
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure}))
	first := mustPrimaryError(t, "sandbox")
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure}))

	repeated := mustPrimaryError(t, "sandbox")
	require.Equal(t, first.Ref, repeated.Ref, "repeat reference changed: %+v", repeated)

	failure.Output = "missing dependency"
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure}))
	changed := mustPrimaryError(t, "sandbox")
	require.NotEqual(t, first.Ref, changed.Ref, "new cause retained reference %q", first.Ref)
	require.NoError(t, ClearGuestPluginFailure("sandbox"))
	s, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, s.Events, 3)
	require.False(t, s.Events[0].Ref != first.Ref)
	require.False(t, s.Events[1].Ref != changed.Ref)
	require.False(t, s.Events[2].Ref != changed.Ref)
	require.True(t, s.Events[2].Resolved)
}

func TestReferenceAllocationRejectsActiveAndRecentCollisions(t *testing.T) {
	s := newStatusStore()
	s.Errors["active"] = ErrorRecord{Ref: "active"}
	s.Events = []EventRecord{{Ref: "recent"}}
	candidates := []string{"active", "recent", "unique"}
	i := 0
	got := unusedErrorRef(&s, func() string {
		ref := candidates[i]
		i++
		return ref
	})
	require.EqualValues(t, "unique", got)
	require.EqualValues(t, 3, i)
}

func TestOlderReferencesAreBackfilledOnUpdate(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, UpdateStatusStore(func(s *StatusStore) {
		s.Errors["older"] = ErrorRecord{Code: CodeRegistration, Scope: "sandbox", Detail: "failed", Count: 2, Dismissed: true}
	}))

	before := mustPrimaryError(t, "sandbox")
	require.Equal(t, "", before.Ref, "fixture not older: %+v", before)

	require.NoError(t, UpdateStatusStore(func(s *StatusStore) {}))
	updated := mustPrimaryError(t, "sandbox")
	require.NotEqualValues(t, "", updated.Ref)
	require.EqualValues(t, 2, updated.Count)
	require.True(t, updated.Dismissed)
	require.NoError(t, UpdateStatusStore(func(s *StatusStore) {}))

	again := mustPrimaryError(t, "sandbox")
	require.Equal(t, updated.Ref, again.Ref, "older reference not durable: %+v", again)
}

func TestPrimaryErrorUsesSeverityThenStablePluginOrder(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, UpdateStatusStore(func(s *StatusStore) {
		s.Errors["warning"] = ErrorRecord{Code: CodeOwnershipConflict, Scope: "sandbox"}
		s.Errors["other-sandbox"] = ErrorRecord{Code: CodeManagement, Scope: "other"}
		s.Errors["last-plugin"] = ErrorRecord{Code: CodeGuestPluginSync, Scope: "sandbox", PluginID: "example.z", Ref: "last00"}
		s.Errors["first-plugin"] = ErrorRecord{Code: CodeSandboxNetwork, Scope: "sandbox", PluginID: "example.a", Ref: "first0", Dismissed: true}
	}))
	for range 10 {
		primary := mustPrimaryError(t, "sandbox")
		require.Equal(t, "first0", primary.Ref, "unstable primary diagnostic: %+v", primary)
	}

	_, found, err := PrimaryError("missing")
	require.NoError(t, err)
	require.False(t, found)
}

func mustPrimaryError(t *testing.T, scope string) ErrorRecord {
	t.Helper()
	entry, found, err := PrimaryError(scope)
	require.NoError(t, err)
	require.True(t, found)
	return entry
}
