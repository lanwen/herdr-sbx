package store

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreparationOutcomeRetainsUnderlyingFailure(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, SetRegistrationError(CodeRegistration, "sandbox", "machine registration failed"))
	require.NoError(t, RecordError(CodePreparation, "sandbox", "Re-sync did not finish: machine registration failed"))
	saved, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 1)
	require.Len(t, saved.Events, 1)
	for _, entry := range saved.Errors {
		require.Equal(t, CodeRegistration, entry.Code, "underlying registration diagnostic replaced: %+v", entry)
	}

	event := saved.Events[0]
	require.False(t, event.Code != CodePreparation)
	require.False(t, SeverityForCode(event.Code) != SeverityInfo)
	require.False(t, event.Resolved)
}

func TestReadStatusNormalizesInformationalOutcomesAndExpiresHistory(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	now := time.Now()
	require.NoError(t, UpdateStatusStore(func(s *StatusStore) {
		s.Errors["legacy"] = ErrorRecord{Ref: "abc123", Code: CodePreparation, Scope: "sandbox", Detail: "Re-sync did not finish", LastSeen: now}
		s.Errors["plugin"] = ErrorRecord{Code: CodeGuestPluginSync, Scope: "sandbox", Detail: "build failed", LastSeen: now}
		s.PreparationErrors["sandbox"] = "build failed"
		s.Preparation["sandbox"] = now.Add(time.Minute)
		s.Events = []EventRecord{{Code: CodeGuestPluginSync, Detail: "expired", At: now.Add(-2 * time.Hour)}, {Code: CodePolicyCleanup, Detail: "recent", At: now.Add(-time.Minute)}}
	}))
	path, err := statusPath()
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	saved, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 1)
	require.Len(t, saved.Events, 2)

	event := saved.Events[1]
	require.False(t, event.Code != CodePreparation)
	require.EqualValues(t, "abc123", event.Ref)

	require.EqualValues(t, "build failed", saved.PreparationErrors["sandbox"])
	require.False(t, saved.Preparation["sandbox"].IsZero())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.False(t, string(before) != string(after))
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
	require.Len(t, s.Events, 0, "events remained after one hour: %+v", s.Events)
}

func TestOlderMigratedOutcomeDoesNotEvictNewerEvents(t *testing.T) {
	now := time.Now()
	s := newStatusStore()
	for i := range 50 {
		appendEvent(&s, EventRecord{Code: CodePolicyCleanup, Detail: "recent", At: now.Add(-time.Duration(49-i) * time.Second)})
	}
	s.Errors["legacy"] = ErrorRecord{Code: CodePreparation, Scope: "sandbox", Detail: "older outcome", LastSeen: now.Add(-30 * time.Minute)}
	normalizeEvents(&s)
	require.Len(t, s.Errors, 0)
	require.Len(t, s.Events, 50)
	for _, event := range s.Events {
		require.Equal(t, "recent", event.Detail, "older migrated event displaced newer history: %+v", event)
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
	require.Len(t, s.Events, 50)
	require.EqualValues(t, "keep tied", s.Events[49].Detail)
	for _, event := range s.Events {
		require.False(t, event.Detail != "recent" && event.Detail != "keep tied", "retained older event: %+v", event)
	}
}
