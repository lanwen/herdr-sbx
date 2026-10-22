package store

import (
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeverityForCode(t *testing.T) {
	for _, code := range []string{CodeOwnershipConflict, CodeRegistration, CodeGuestPluginSync, CodeDaemonStop, "", "future-code"} {
		want := SeverityError
		if code == CodeOwnershipConflict {
			want = SeverityWarning
		}

		got := SeverityForCode(code)
		assert.Equal(t, want, got, "%q severity = %s, want %s", code, got, want)
	}
}

func TestGuestPluginFailureHistoryTracksTransitions(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	require.NoError(t, ObserveSandbox("daemon", sbx.Sandbox{ID: "sandbox", Name: "demo"}))
	for range 2 {
		require.NoError(t, RecordGuestPluginFailure(scope, "guest.plugin: missing dependency"))
	}
	require.NoError(t, RecordGuestPluginFailure(scope, "guest.plugin: install rejected"))
	require.NoError(t, ClearGuestPluginFailure(scope))
	require.NoError(t, ClearGuestPluginFailure(scope))
	require.NoError(t, RecordGuestPluginFailure(scope, "guest.plugin: missing dependency"))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Events, 4)
	require.Len(t, status.Errors, 1)
	for i, want := range []struct {
		detail   string
		resolved bool
	}{
		{"guest.plugin: missing dependency", false},
		{"guest.plugin: install rejected", false},
		{"Guest plugin sync recovered", true},
		{"guest.plugin: missing dependency", false},
	} {
		got := status.Events[i]
		require.False(t, got.Code != CodeGuestPluginSync)
		require.False(t, got.Scope != scope)
		require.EqualValues(t, "demo", got.Name)
		require.False(t, got.Detail != want.detail)
		require.False(t, got.Resolved != want.resolved)
		require.False(t, got.At.IsZero())
	}
}

func TestGuestPluginFailureCodeTracksCauseChange(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	require.NoError(t, RecordGuestPluginFailureCode(CodeGuestPluginResponse, scope, "bad frame"))
	require.NoError(t, RecordGuestPluginFailureCode(CodeGuestPluginSync, scope, "install failed"))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Events, 2)
	require.Len(t, status.Errors, 1)
	require.False(t, status.Events[0].Code != CodeGuestPluginResponse)
	require.False(t, status.Events[1].Code != CodeGuestPluginSync)
}

func TestRegistrationFailureReplacesPreviousOutcome(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	require.NoError(t, SetRegistrationError(CodeOwnershipConflict, scope, "manual machine exists"))
	require.NoError(t, SetRegistrationError(CodeWorkspaceSetup, scope, "workspace failed"))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1)
	for _, record := range status.Errors {
		require.Equal(t, CodeWorkspaceSetup, record.Code, "stale outcome retained: %+v", record)
	}
	require.NoError(t, SetRegistrationError("", scope, ""))
	status, err = ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 0)
}

func TestGuestPluginFailureHistoryIsBounded(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for i := range 60 {
		require.NoError(t, RecordGuestPluginFailure(StatusKey("daemon", string(rune(i+65))), "failure"))
	}
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Events, 50, "event count = %d", len(status.Events))
}

func TestPluginFailuresHaveIndependentAttemptsAndRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", Recovery: "Install in the sandbox.", Command: "herdr plugin install owner/repo --ref abc --yes", OccurredAt: time.Now().Add(-time.Minute)}
	second := PluginFailure{PluginID: "example.second", Code: CodeGuestPluginResponse, Detail: "Invalid guest response", OccurredAt: first.OccurredAt.Add(time.Second)}
	for range 3 {
		require.NoError(t, SetGuestPluginFailures(scope, []PluginFailure{first, second}))
	}
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 2)
	require.Len(t, status.Events, 2)
	for _, entry := range status.Errors {
		require.Equal(t, 1, entry.Count, "observation counted as an attempt: %+v", entry)
		require.False(t, entry.PluginID == first.PluginID && (entry.Output != first.Output || entry.Command != first.Command || !entry.LastSeen.Equal(first.OccurredAt)), "metadata lost: %+v", entry)
	}
	first.OccurredAt = first.OccurredAt.Add(30 * time.Second)
	require.NoError(t, SetGuestPluginFailures(scope, []PluginFailure{first}))
	status, _ = ReadStatusStore()
	require.Len(t, status.Errors, 1)
	require.Len(t, status.Events, 3)
	for _, entry := range status.Errors {
		require.False(t, entry.PluginID != first.PluginID)
		require.EqualValues(t, 2, entry.Count)
	}
	recovered := status.Events[2]
	require.True(t, recovered.Resolved)
	require.False(t, recovered.PluginID != second.PluginID)
	require.False(t, recovered.Scope != scope)
	require.NoError(t, SetGuestPluginFailures(scope, nil))
	status, _ = ReadStatusStore()
	require.Len(t, status.Errors, 0)
	require.Len(t, status.Events, 4)
	require.False(t, status.Events[3].PluginID != first.PluginID)
}

func TestPluginDiscoveryFailureDoesNotResolveIndividualFailures(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed"}
	require.NoError(t, SetGuestPluginFailures(scope, []PluginFailure{first}))
	require.NoError(t, RecordGuestPluginFailureCode(CodeGuestPluginSync, scope, "Local catalog unavailable"))
	status, _ := ReadStatusStore()
	require.Len(t, status.Errors, 2)
	require.Len(t, status.Events, 2)
	first.Code = CodeSandboxNetwork
	first.Detail = "Policy blocked download"
	require.NoError(t, SetGuestPluginFailures(scope, []PluginFailure{first}))
	status, _ = ReadStatusStore()
	require.Len(t, status.Errors, 1, "stale cause retained: %+v", status.Errors)
	for _, event := range status.Events {
		require.False(t, event.PluginID == first.PluginID && event.Resolved, "still-failed plugin marked recovered: %+v", event)
	}
}

func TestClearOperationErrorKeepsPluginSiblings(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	require.NoError(t, SetGuestPluginFailures(scope, []PluginFailure{{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed"}}))
	require.NoError(t, RecordError(CodeGuestPluginSync, scope, "Operation failed"))
	require.NoError(t, ClearError(CodeGuestPluginSync, scope))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1, "plugin siblings cleared: %+v", status.Errors)
	for _, entry := range status.Errors {
		require.Equal(t, "example.first", entry.PluginID, "wrong error retained: %+v", entry)
	}
}

func TestDismissedOperationRemainsActiveAcrossRepeatedObservations(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, RecordError(CodeManagement, "sandbox", "request rejected"))
	status, _ := ReadStatusStore()
	for _, entry := range status.Errors {
		require.NoError(t, DismissError(entry))
	}
	require.NoError(t, RecordError(CodeManagement, "sandbox", "request rejected"))
	require.NoError(t, RecordError(CodeManagement, "sandbox", "connection lost"))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 2)
	for _, entry := range status.Errors {
		require.False(t, entry.Detail == "request rejected" && (!entry.Dismissed || entry.Count != 2), "dismissal lost: %+v", entry)
		require.False(t, entry.Detail == "connection lost" && !entry.Dismissed, "changed cause lost group dismissal: %+v", entry)
	}
	require.NoError(t, ClearError(CodeManagement, "sandbox"))
	require.NoError(t, RecordError(CodeManagement, "sandbox", "request rejected"))
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		require.False(t, entry.Dismissed)
		require.EqualValues(t, 1, entry.Count)
	}
}

func TestPluginDismissalPreservesIdentityAndRejectsStaleCause(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", OccurredAt: time.Now()}
	sibling := failure
	sibling.PluginID = "example.second"
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}))
	status, _ := ReadStatusStore()
	var selected ErrorRecord
	for _, entry := range status.Errors {
		if entry.PluginID == failure.PluginID {
			selected = entry
		}
	}
	require.NoError(t, DismissError(selected))
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}))
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		require.False(t, entry.PluginID == failure.PluginID && (!entry.Dismissed || entry.Count != 2), "repeat lost dismissal: %+v", entry)
		require.False(t, entry.PluginID == sibling.PluginID && !entry.Dismissed, "sibling not dismissed with group: %+v", entry)
	}
	failure.Output = "new build failure"
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}))
	require.NoError(t, DismissError(selected))
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		require.True(t, entry.Dismissed, "changed cause lost group dismissal: %+v", entry)
	}
}

func TestDismissErrorDoesNotAcknowledgeReappearingIdenticalFailure(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.plugin", Code: CodeGuestPluginSync, Detail: "Build failed", OccurredAt: time.Now().Add(-time.Minute)}
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure}))
	status, _ := ReadStatusStore()
	var selected ErrorRecord
	for _, entry := range status.Errors {
		selected = entry
	}
	require.NoError(t, ClearGuestPluginFailure("sandbox"))
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	require.NoError(t, SetGuestPluginFailures("sandbox", []PluginFailure{failure}))
	require.NoError(t, DismissError(selected))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1)
	for _, entry := range status.Errors {
		require.False(t, entry.Dismissed, "stale selection acknowledged new lifecycle: %+v", entry)
	}
}

func TestPolicyCleanupCheckedIsNamedBoundedEvent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := "daemon-scope-hash"
	for range 51 {
		require.NoError(t, RecordPolicyCleanupChecked(scope))
	}
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Events, 50)
	require.Len(t, status.Errors, 0)
	for _, event := range status.Events {
		require.False(t, event.Code != CodePolicyCleanup)
		require.False(t, event.Scope != scope)
		require.EqualValues(t, "All sandboxes", event.Name)
		require.True(t, event.Resolved)
		require.False(t, event.At.IsZero())
		require.EqualValues(t, "Temporary access cleanup checked", event.Detail)
	}
}
