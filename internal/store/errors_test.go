package store

import (
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func TestSeverityForCode(t *testing.T) {
	for _, code := range []string{CodeOwnershipConflict, CodeRegistration, CodeGuestPluginSync, CodeDaemonStop, "", "future-code"} {
		want := SeverityError
		if code == CodeOwnershipConflict {
			want = SeverityWarning
		}
		if got := SeverityForCode(code); got != want {
			t.Errorf("%q severity = %s, want %s", code, got, want)
		}
	}
}

func TestGuestPluginFailureHistoryTracksTransitions(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	if err := ObserveSandbox("daemon", sbx.Sandbox{ID: "sandbox", Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RecordGuestPluginFailure(scope, "guest.plugin: missing dependency"); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordGuestPluginFailure(scope, "guest.plugin: install rejected"); err != nil {
		t.Fatal(err)
	}
	if err := ClearGuestPluginFailure(scope); err != nil {
		t.Fatal(err)
	}
	if err := ClearGuestPluginFailure(scope); err != nil {
		t.Fatal(err)
	}
	if err := RecordGuestPluginFailure(scope, "guest.plugin: missing dependency"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Events) != 4 || len(status.Errors) != 1 {
		t.Fatalf("events=%+v errors=%+v", status.Events, status.Errors)
	}
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
		if got.Code != CodeGuestPluginSync || got.Scope != scope || got.Name != "demo" || got.Detail != want.detail || got.Resolved != want.resolved || got.At.IsZero() {
			t.Fatalf("event %d: %+v", i, got)
		}
	}
}

func TestGuestPluginFailureCodeTracksCauseChange(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	if err := RecordGuestPluginFailureCode(CodeGuestPluginResponse, scope, "bad frame"); err != nil {
		t.Fatal(err)
	}
	if err := RecordGuestPluginFailureCode(CodeGuestPluginSync, scope, "install failed"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Events) != 2 || len(status.Errors) != 1 || status.Events[0].Code != CodeGuestPluginResponse || status.Events[1].Code != CodeGuestPluginSync {
		t.Fatalf("cause transition: status=%+v err=%v", status, err)
	}
}

func TestRegistrationFailureReplacesPreviousOutcome(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	if err := SetRegistrationError(CodeOwnershipConflict, scope, "manual machine exists"); err != nil {
		t.Fatal(err)
	}
	if err := SetRegistrationError(CodeWorkspaceSetup, scope, "workspace failed"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Errors) != 1 {
		t.Fatalf("current outcome: status=%+v err=%v", status, err)
	}
	for _, record := range status.Errors {
		if record.Code != CodeWorkspaceSetup {
			t.Fatalf("stale outcome retained: %+v", record)
		}
	}
	if err := SetRegistrationError("", scope, ""); err != nil {
		t.Fatal(err)
	}
	status, err = ReadStatusStore()
	if err != nil || len(status.Errors) != 0 {
		t.Fatalf("ready outcome: status=%+v err=%v", status, err)
	}
}

func TestGuestPluginFailureHistoryIsBounded(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for i := range 60 {
		if err := RecordGuestPluginFailure(StatusKey("daemon", string(rune(i+65))), "failure"); err != nil {
			t.Fatal(err)
		}
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Events) != 50 {
		t.Fatalf("event count = %d", len(status.Events))
	}
}

func TestPluginFailuresHaveIndependentAttemptsAndRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", Recovery: "Install in the sandbox.", Command: "herdr plugin install owner/repo --ref abc --yes", OccurredAt: time.Now().Add(-time.Minute)}
	second := PluginFailure{PluginID: "example.second", Code: CodeGuestPluginResponse, Detail: "Invalid guest response", OccurredAt: first.OccurredAt.Add(time.Second)}
	for range 3 {
		if err := SetGuestPluginFailures(scope, []PluginFailure{first, second}); err != nil {
			t.Fatal(err)
		}
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Errors) != 2 || len(status.Events) != 2 {
		t.Fatalf("independent failures: %+v", status)
	}
	for _, entry := range status.Errors {
		if entry.Count != 1 {
			t.Fatalf("observation counted as an attempt: %+v", entry)
		}
		if entry.PluginID == first.PluginID && (entry.Output != first.Output || entry.Command != first.Command || !entry.LastSeen.Equal(first.OccurredAt)) {
			t.Fatalf("metadata lost: %+v", entry)
		}
	}
	first.OccurredAt = first.OccurredAt.Add(30 * time.Second)
	if err := SetGuestPluginFailures(scope, []PluginFailure{first}); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	if len(status.Errors) != 1 || len(status.Events) != 3 {
		t.Fatalf("independent recovery: %+v", status)
	}
	for _, entry := range status.Errors {
		if entry.PluginID != first.PluginID || entry.Count != 2 {
			t.Fatalf("retry count: %+v", entry)
		}
	}
	recovered := status.Events[2]
	if !recovered.Resolved || recovered.PluginID != second.PluginID || recovered.Scope != scope {
		t.Fatalf("recovery identity: %+v", recovered)
	}
	if err := SetGuestPluginFailures(scope, nil); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	if len(status.Errors) != 0 || len(status.Events) != 4 || status.Events[3].PluginID != first.PluginID {
		t.Fatalf("last recovery: %+v", status)
	}
}

func TestPluginDiscoveryFailureDoesNotResolveIndividualFailures(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed"}
	if err := SetGuestPluginFailures(scope, []PluginFailure{first}); err != nil {
		t.Fatal(err)
	}
	if err := RecordGuestPluginFailureCode(CodeGuestPluginSync, scope, "Local catalog unavailable"); err != nil {
		t.Fatal(err)
	}
	status, _ := ReadStatusStore()
	if len(status.Errors) != 2 || len(status.Events) != 2 {
		t.Fatalf("discovery erased known failure: %+v", status)
	}
	first.Code = CodeSandboxNetwork
	first.Detail = "Policy blocked download"
	if err := SetGuestPluginFailures(scope, []PluginFailure{first}); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	if len(status.Errors) != 1 {
		t.Fatalf("stale cause retained: %+v", status.Errors)
	}
	for _, event := range status.Events {
		if event.PluginID == first.PluginID && event.Resolved {
			t.Fatalf("still-failed plugin marked recovered: %+v", event)
		}
	}
}

func TestClearOperationErrorKeepsPluginSiblings(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	if err := SetGuestPluginFailures(scope, []PluginFailure{{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed"}}); err != nil {
		t.Fatal(err)
	}
	if err := RecordError(CodeGuestPluginSync, scope, "Operation failed"); err != nil {
		t.Fatal(err)
	}
	if err := ClearError(CodeGuestPluginSync, scope); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Errors) != 1 {
		t.Fatalf("plugin siblings cleared: %+v", status.Errors)
	}
	for _, entry := range status.Errors {
		if entry.PluginID != "example.first" {
			t.Fatalf("wrong error retained: %+v", entry)
		}
	}
}

func TestDismissedOperationRemainsActiveAcrossRepeatedObservations(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := RecordError(CodeManagement, "sandbox", "request rejected"); err != nil {
		t.Fatal(err)
	}
	status, _ := ReadStatusStore()
	for _, entry := range status.Errors {
		if err := DismissError(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordError(CodeManagement, "sandbox", "request rejected"); err != nil {
		t.Fatal(err)
	}
	if err := RecordError(CodeManagement, "sandbox", "connection lost"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Errors) != 2 {
		t.Fatalf("active errors=%+v err=%v", status.Errors, err)
	}
	for _, entry := range status.Errors {
		if entry.Detail == "request rejected" && (!entry.Dismissed || entry.Count != 2) {
			t.Fatalf("dismissal lost: %+v", entry)
		}
		if entry.Detail == "connection lost" && !entry.Dismissed {
			t.Fatalf("changed cause lost group dismissal: %+v", entry)
		}
	}
	if err := ClearError(CodeManagement, "sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := RecordError(CodeManagement, "sandbox", "request rejected"); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		if entry.Dismissed || entry.Count != 1 {
			t.Fatalf("new failure inherited resolved acknowledgement: %+v", entry)
		}
	}
}

func TestPluginDismissalPreservesIdentityAndRejectsStaleCause(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", OccurredAt: time.Now()}
	sibling := failure
	sibling.PluginID = "example.second"
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}); err != nil {
		t.Fatal(err)
	}
	status, _ := ReadStatusStore()
	var selected ErrorRecord
	for _, entry := range status.Errors {
		if entry.PluginID == failure.PluginID {
			selected = entry
		}
	}
	if err := DismissError(selected); err != nil {
		t.Fatal(err)
	}
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		if entry.PluginID == failure.PluginID && (!entry.Dismissed || entry.Count != 2) {
			t.Fatalf("repeat lost dismissal: %+v", entry)
		}
		if entry.PluginID == sibling.PluginID && !entry.Dismissed {
			t.Fatalf("sibling not dismissed with group: %+v", entry)
		}
	}
	failure.Output = "new build failure"
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure, sibling}); err != nil {
		t.Fatal(err)
	}
	if err := DismissError(selected); err != nil {
		t.Fatal(err)
	}
	status, _ = ReadStatusStore()
	for _, entry := range status.Errors {
		if !entry.Dismissed {
			t.Fatalf("changed cause lost group dismissal: %+v", entry)
		}
	}
}

func TestDismissErrorDoesNotAcknowledgeReappearingIdenticalFailure(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.plugin", Code: CodeGuestPluginSync, Detail: "Build failed", OccurredAt: time.Now().Add(-time.Minute)}
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	status, _ := ReadStatusStore()
	var selected ErrorRecord
	for _, entry := range status.Errors {
		selected = entry
	}
	if err := ClearGuestPluginFailure("sandbox"); err != nil {
		t.Fatal(err)
	}
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	if err := DismissError(selected); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Errors) != 1 {
		t.Fatalf("reappeared failure: %+v, err=%v", status.Errors, err)
	}
	for _, entry := range status.Errors {
		if entry.Dismissed {
			t.Fatalf("stale selection acknowledged new lifecycle: %+v", entry)
		}
	}
}

func TestPolicyCleanupCheckedIsNamedBoundedEvent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := "daemon-scope-hash"
	for range 51 {
		if err := RecordPolicyCleanupChecked(scope); err != nil {
			t.Fatal(err)
		}
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Events) != 50 || len(status.Errors) != 0 {
		t.Fatalf("cleanup history=%+v err=%v", status, err)
	}
	for _, event := range status.Events {
		if event.Code != CodePolicyCleanup || event.Scope != scope || event.Name != "All sandboxes" || !event.Resolved || event.At.IsZero() || event.Detail != "Temporary access cleanup checked" {
			t.Fatalf("cleanup event: %+v", event)
		}
	}
}
