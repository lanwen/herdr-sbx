package store

import "testing"

func TestDismissedPluginGroupSurvivesMembershipChangesUntilCompleteRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	first := PluginFailure{PluginID: "example.first", Code: CodeGuestPluginSync, Detail: "build failed"}
	second := PluginFailure{PluginID: "example.second", Code: CodeGuestPluginSync, Detail: "other failure"}
	mustUpdate := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	selected := mustPrimaryError(t, "sandbox")
	mustUpdate(DismissError(selected))
	// Replacing every plugin in one observation is not complete recovery.
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{second}))
	if entry := mustPrimaryError(t, "sandbox"); !entry.Dismissed || entry.PluginID != second.PluginID || entry.Ref == selected.Ref {
		t.Fatalf("replacement did not retain group acknowledgement: %+v", entry)
	}
	first.Detail = "changed build failure"
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first, second}))
	status, err := ReadStatusStore()
	mustUpdate(err)
	for _, entry := range status.Errors {
		if !entry.Dismissed {
			t.Fatalf("new child appeared in dismissed group: %+v", entry)
		}
	}
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	if entry := mustPrimaryError(t, "sandbox"); !entry.Dismissed {
		t.Fatalf("partial recovery removed acknowledgement: %+v", entry)
	}
	mustUpdate(ClearGuestPluginFailure("sandbox"))
	status, err = ReadStatusStore()
	mustUpdate(err)
	if len(status.DismissedErrorGroups) != 0 {
		t.Fatalf("recovered group retained acknowledgement: %+v", status.DismissedErrorGroups)
	}
	mustUpdate(SetGuestPluginFailures("sandbox", []PluginFailure{first}))
	if entry := mustPrimaryError(t, "sandbox"); entry.Dismissed {
		t.Fatalf("reappearing group hidden: %+v", entry)
	}
}

func TestRegistrationDismissalSurvivesCauseReplacementAndOtherStatusWrites(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := SetRegistrationError(CodeRegistration, "sandbox", "old cause"); err != nil {
		t.Fatal(err)
	}
	if err := DismissError(mustPrimaryError(t, "sandbox")); err != nil {
		t.Fatal(err)
	}
	if err := SetRegistrationError(CodeRegistration, "sandbox", "changed cause"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateStatusStore(func(s *StatusStore) { s.Warnings["unrelated"] = "changed" }); err != nil {
		t.Fatal(err)
	}
	if entry := mustPrimaryError(t, "sandbox"); !entry.Dismissed || entry.Detail != "changed cause" {
		t.Fatalf("replacement lost acknowledgement: %+v", entry)
	}
	if err := SetRegistrationError(CodeSandboxSetup, "sandbox", "different code"); err != nil {
		t.Fatal(err)
	}
	if entry := mustPrimaryError(t, "sandbox"); entry.Dismissed {
		t.Fatalf("different code inherited acknowledgement: %+v", entry)
	}
	if err := SetRegistrationError(CodeRegistration, "sandbox", "old cause"); err != nil {
		t.Fatal(err)
	}
	if entry := mustPrimaryError(t, "sandbox"); entry.Dismissed {
		t.Fatalf("recovered code inherited acknowledgement: %+v", entry)
	}
}

func TestDismissalDoesNotCrossScopeOrCode(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for _, item := range []struct{ scope, code string }{{"one", CodeManagement}, {"one", CodeRegistration}, {"two", CodeManagement}} {
		if err := RecordError(item.code, item.scope, "failed"); err != nil {
			t.Fatal(err)
		}
	}
	status, _ := ReadStatusStore()
	for _, entry := range status.Errors {
		if entry.Scope == "one" && entry.Code == CodeManagement {
			if err := DismissError(entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range status.Errors {
		if want := entry.Scope == "one" && entry.Code == CodeManagement; entry.Dismissed != want {
			t.Fatalf("incorrect group acknowledgement: %+v", entry)
		}
	}
}

func TestLegacyDismissalImportsOnlyFullyDismissedGroups(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "mixed"}[mixed], func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			if err := UpdateStatusStore(func(s *StatusStore) {
				s.Errors["first"] = ErrorRecord{Scope: "sandbox", Code: CodeGuestPluginSync, Dismissed: true}
				s.Errors["second"] = ErrorRecord{Scope: "sandbox", Code: CodeGuestPluginSync, Dismissed: !mixed}
			}); err != nil {
				t.Fatal(err)
			}
			status, err := ReadStatusStore()
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range status.Errors {
				if entry.Dismissed == mixed {
					t.Fatalf("incorrect imported acknowledgement: %+v", entry)
				}
			}
		})
	}
}
