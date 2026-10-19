package store

import (
	"regexp"
	"testing"
	"time"
)

func TestErrorReferenceSurvivesRepeatDismissalAndChangesAfterRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := RecordError(CodeManagement, "sandbox", "cannot update"); err != nil {
		t.Fatal(err)
	}
	first := mustPrimaryError(t, "sandbox")
	if !regexp.MustCompile(`^[a-z0-9]{6}$`).MatchString(first.Ref) {
		t.Fatalf("invalid reference %q", first.Ref)
	}
	if err := DismissError(first); err != nil {
		t.Fatal(err)
	}
	if err := RecordError(CodeManagement, "sandbox", "cannot update"); err != nil {
		t.Fatal(err)
	}
	repeated := mustPrimaryError(t, "sandbox")
	if repeated.Ref != first.Ref || !repeated.Dismissed || repeated.Count != 2 {
		t.Fatalf("repeat identity changed: first=%+v repeated=%+v", first, repeated)
	}
	if err := ClearError(CodeManagement, "sandbox"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := PrimaryError("sandbox"); err != nil || found {
		t.Fatalf("cleared diagnostic: found=%v err=%v", found, err)
	}
	if err := RecordError(CodeManagement, "sandbox", "cannot update"); err != nil {
		t.Fatal(err)
	}
	if current := mustPrimaryError(t, "sandbox"); current.Ref == first.Ref || current.Dismissed {
		t.Fatalf("recovery retained old identity: %+v", current)
	}
}

func TestPluginReferenceCorrelatesFailureRecoveryAndChangedCause(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	failure := PluginFailure{PluginID: "example.plugin", Code: CodeGuestPluginSync, Detail: "Build failed", Output: "missing compiler", OccurredAt: time.Now()}
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	first := mustPrimaryError(t, "sandbox")
	failure.OccurredAt = failure.OccurredAt.Add(time.Minute)
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	if repeated := mustPrimaryError(t, "sandbox"); repeated.Ref != first.Ref {
		t.Fatalf("repeat reference changed: %+v", repeated)
	}
	failure.Output = "missing dependency"
	if err := SetGuestPluginFailures("sandbox", []PluginFailure{failure}); err != nil {
		t.Fatal(err)
	}
	changed := mustPrimaryError(t, "sandbox")
	if changed.Ref == first.Ref {
		t.Fatalf("new cause retained reference %q", first.Ref)
	}
	if err := ClearGuestPluginFailure("sandbox"); err != nil {
		t.Fatal(err)
	}
	s, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 3 || s.Events[0].Ref != first.Ref || s.Events[1].Ref != changed.Ref || s.Events[2].Ref != changed.Ref || !s.Events[2].Resolved {
		t.Fatalf("uncorrelated history: %+v", s.Events)
	}
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
	if got != "unique" || i != 3 {
		t.Fatalf("reference=%q attempts=%d", got, i)
	}
}

func TestLegacyReferencesAreBackfilledOnUpdate(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := UpdateStatusStore(func(s *StatusStore) {
		s.Errors["legacy"] = ErrorRecord{Code: CodeRegistration, Scope: "sandbox", Detail: "failed", Count: 2, Dismissed: true}
	}); err != nil {
		t.Fatal(err)
	}
	if before := mustPrimaryError(t, "sandbox"); before.Ref != "" {
		t.Fatalf("fixture not legacy: %+v", before)
	}
	if err := UpdateStatusStore(func(s *StatusStore) {}); err != nil {
		t.Fatal(err)
	}
	updated := mustPrimaryError(t, "sandbox")
	if updated.Ref == "" || updated.Count != 2 || !updated.Dismissed {
		t.Fatalf("legacy migration changed outcome: %+v", updated)
	}
	if err := UpdateStatusStore(func(s *StatusStore) {}); err != nil {
		t.Fatal(err)
	}
	if again := mustPrimaryError(t, "sandbox"); again.Ref != updated.Ref {
		t.Fatalf("legacy reference not durable: %+v", again)
	}
}

func TestPrimaryErrorUsesSeverityThenStablePluginOrder(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := UpdateStatusStore(func(s *StatusStore) {
		s.Errors["warning"] = ErrorRecord{Code: CodeOwnershipConflict, Scope: "sandbox"}
		s.Errors["other-sandbox"] = ErrorRecord{Code: CodeManagement, Scope: "other"}
		s.Errors["last-plugin"] = ErrorRecord{Code: CodeGuestPluginSync, Scope: "sandbox", PluginID: "example.z", Ref: "last00"}
		s.Errors["first-plugin"] = ErrorRecord{Code: CodeSandboxNetwork, Scope: "sandbox", PluginID: "example.a", Ref: "first0", Dismissed: true}
	}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if primary := mustPrimaryError(t, "sandbox"); primary.Ref != "first0" {
			t.Fatalf("unstable primary diagnostic: %+v", primary)
		}
	}
	if _, found, err := PrimaryError("missing"); err != nil || found {
		t.Fatalf("missing scope found=%v err=%v", found, err)
	}
}

func mustPrimaryError(t *testing.T, scope string) ErrorRecord {
	t.Helper()
	entry, found, err := PrimaryError(scope)
	if err != nil || !found {
		t.Fatalf("primary error: found=%v err=%v", found, err)
	}
	return entry
}
