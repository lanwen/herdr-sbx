package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDismissedPluginGroupSurvivesMembershipChangesUntilCompleteRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "build failed"}
	second := PluginFailure{PluginID: "example.second", Code: CodeGuestPluginSync, Detail: "other failure"}
	mustUpdate := func(err error) {
		t.Helper()
		require.NoError(t, err)
	}
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	selected := mustPrimaryError(t, "sandbox")
	mustUpdate(DismissError(selected))
	// Replacing every plugin in one observation is not complete recovery.
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{second}))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.True(t, entry.Dismissed)
		require.False(t, entry.PluginID != second.PluginID)
		require.False(t, entry.Ref == selected.Ref)
	}
	first.Detail = "changed build failure"
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first, second}))
	status, err := ReadStatusStore()
	mustUpdate(err)
	for _, entry := range status.Errors {
		require.True(t, entry.Dismissed, "new child appeared in dismissed group: %+v", entry)
	}
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.True(t, entry.Dismissed, "partial recovery removed acknowledgement: %+v", entry)
	}
	mustUpdate(ClearGuestPluginFailure("sandbox"))
	status, err = ReadStatusStore()
	mustUpdate(err)
	require.Len(t, status.DismissedErrorGroups, 0, "recovered group retained acknowledgement: %+v", status.DismissedErrorGroups)
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.False(t, entry.Dismissed, "reappearing group hidden: %+v", entry)
	}
}

func TestRegistrationDismissalSurvivesCauseReplacementAndOtherStatusWrites(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, SetRegistrationError(CodeRegistration, "sandbox", "old cause"))
	require.NoError(t, DismissError(mustPrimaryError(t, "sandbox")))
	require.NoError(t, SetRegistrationError(CodeRegistration, "sandbox", "changed cause"))
	require.NoError(t, UpdateStatusStore(func(s *StatusStore) { s.Warnings["unrelated"] = "changed" }))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.True(t, entry.Dismissed)
		require.EqualValues(t, "changed cause", entry.Detail)
	}
	require.NoError(t, SetRegistrationError(CodeSandboxSetup, "sandbox", "different code"))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.False(t, entry.Dismissed, "different code inherited acknowledgement: %+v", entry)
	}
	require.NoError(t, SetRegistrationError(CodeRegistration, "sandbox", "old cause"))
	{
		entry := mustPrimaryError(t, "sandbox")
		require.False(t, entry.Dismissed, "recovered code inherited acknowledgement: %+v", entry)
	}
}

func TestDismissalDoesNotCrossScopeOrCode(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for _, item := range []struct{ scope, code string }{{"one", CodeManagement}, {"one", CodeRegistration}, {"two", CodeManagement}} {
		require.NoError(t, RecordError(item.code, item.scope, "failed"))
	}
	status, _ := ReadStatusStore()
	for _, entry := range status.Errors {
		if entry.Scope == "one" && entry.Code == CodeManagement {
			require.NoError(t, DismissError(entry))
		}
	}
	status, err := ReadStatusStore()
	require.NoError(t, err)
	for _, entry := range status.Errors {
		want := entry.Scope == "one" && entry.Code == CodeManagement
		require.Equal(t, want, entry.Dismissed, "incorrect group acknowledgement: %+v", entry)
	}
}

func TestLegacyDismissalImportsOnlyFullyDismissedGroups(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "mixed"}[mixed], func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			require.NoError(t, UpdateStatusStore(func(s *StatusStore) {
				s.Errors["first"] = ErrorRecord{Scope: "sandbox", Code: CodeGuestPluginSync, Dismissed: true}
				s.Errors["second"] = ErrorRecord{Scope: "sandbox", Code: CodeGuestPluginSync, Dismissed: !mixed}
			}))
			status, err := ReadStatusStore()
			require.NoError(t, err)
			for _, entry := range status.Errors {
				require.NotEqual(t, mixed, entry.Dismissed, "incorrect imported acknowledgement: %+v", entry)
			}
		})
	}
}
