package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestPreparationOutcomeRetainsUnderlyingFailure(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := SetRegistrationError(CodeRegistration, "sandbox", "machine registration failed"); err != nil {
		t.Fatal(err)
	}
	if err := RecordError(CodePreparation, "sandbox", "Re-sync did not finish: machine registration failed"); err != nil {
		t.Fatal(err)
	}
	saved, err := ReadStatusStore()
	if err != nil || len(saved.Errors) != 1 || len(saved.Events) != 1 {
		t.Fatalf("outcomes: %+v, %v", saved, err)
	}
	for _, entry := range saved.Errors {
		if entry.Code != CodeRegistration {
			t.Fatalf("underlying registration diagnostic replaced: %+v", entry)
		}
	}
	if event := saved.Events[0]; event.Code != CodePreparation || SeverityForCode(event.Code) != SeverityInfo || event.Resolved {
		t.Fatalf("re-sync outcome is not informational: %+v", event)
	}
}

func TestReadStatusNormalizesInformationalOutcomesAndExpiresHistory(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	now := time.Now()
	if err := UpdateStatusStore(func(s *StatusStore) {
		s.Errors["legacy"] = ErrorRecord{Ref: "abc123", Code: CodePreparation, Scope: "sandbox", Detail: "Re-sync did not finish", LastSeen: now}
		s.Errors["plugin"] = ErrorRecord{Code: CodeGuestPluginSync, Scope: "sandbox", Detail: "build failed", LastSeen: now}
		s.PreparationErrors["sandbox"] = "build failed"
		s.Preparation["sandbox"] = now.Add(time.Minute)
		s.Events = []EventRecord{{Code: CodeGuestPluginSync, Detail: "expired", At: now.Add(-2 * time.Hour)}, {Code: CodePolicyCleanup, Detail: "recent", At: now.Add(-time.Minute)}}
	}); err != nil {
		t.Fatal(err)
	}
	path, err := statusPath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := ReadStatusStore()
	if err != nil || len(saved.Errors) != 1 || len(saved.Events) != 2 {
		t.Fatalf("normalized history: %+v, %v", saved, err)
	}
	if event := saved.Events[1]; event.Code != CodePreparation || event.Ref != "abc123" {
		t.Fatalf("legacy outcome was not preserved: %+v", event)
	}
	if saved.PreparationErrors["sandbox"] != "build failed" || saved.Preparation["sandbox"].IsZero() {
		t.Fatal("normalization changed the retry cooldown")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("reading event history wrote the journal")
	}
}

func TestEventRetentionKeepsNewestFiftyWithinOneHour(t *testing.T) {
	now := time.Now()
	s := newStatusStore()
	for i := range 60 {
		appendEvent(&s, EventRecord{Detail: "current", At: now.Add(-time.Duration(59-i) * time.Second)})
	}
	appendEvent(&s, EventRecord{Detail: "expired", At: now.Add(-2 * time.Hour)})
	if len(s.Events) != 50 || !s.Events[0].At.Equal(now.Add(-49*time.Second)) || !s.Events[49].At.Equal(now) {
		data, _ := json.Marshal(s.Events)
		t.Fatalf("wrong retained history: %s", data)
	}
	retainEvents(&s, now.Add(time.Hour))
	if len(s.Events) != 0 {
		t.Fatalf("events remained after one hour: %+v", s.Events)
	}
}

func TestOlderMigratedOutcomeDoesNotEvictNewerEvents(t *testing.T) {
	now := time.Now()
	s := newStatusStore()
	for i := range 50 {
		appendEvent(&s, EventRecord{Code: CodePolicyCleanup, Detail: "recent", At: now.Add(-time.Duration(49-i) * time.Second)})
	}
	s.Errors["legacy"] = ErrorRecord{Code: CodePreparation, Scope: "sandbox", Detail: "older outcome", LastSeen: now.Add(-30 * time.Minute)}
	normalizeEvents(&s)
	if len(s.Errors) != 0 || len(s.Events) != 50 {
		t.Fatalf("migration: errors=%+v events=%+v", s.Errors, s.Events)
	}
	for _, event := range s.Events {
		if event.Detail != "recent" {
			t.Fatalf("older migrated event displaced newer history: %+v", event)
		}
	}
}

func TestEventCapRanksMixedTimestampsWithStableTies(t *testing.T) {
	now := time.Now()
	s := newStatusStore()
	// The first two entries tie at the cutoff. Stable ranking discards the
	// earlier appended tie and retains the later one.
	s.Events = []EventRecord{{Detail: "discard tied", At: now.Add(-time.Minute)}}
	for range 49 {
		s.Events = append(s.Events, EventRecord{Detail: "recent", At: now})
	}
	s.Events = append(s.Events, EventRecord{Detail: "keep tied", At: now.Add(-time.Minute)}, EventRecord{Detail: "discard old", At: now.Add(-30 * time.Minute)})
	retainEvents(&s, now)
	if len(s.Events) != 50 || s.Events[49].Detail != "keep tied" {
		t.Fatalf("retention changed ordering or tied cutoff: %+v", s.Events)
	}
	for _, event := range s.Events {
		if event.Detail != "recent" && event.Detail != "keep tied" {
			t.Fatalf("retained older event: %+v", event)
		}
	}
}
