package store

import "testing"

func TestHerdrFixFailureNeedsExplicitRecovery(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	scope := StatusKey("daemon", "sandbox")
	if SeverityForCode(CodeHerdrFix) != SeverityError {
		t.Fatal("repair failure is not an error")
	}
	if err := RecordError(CodeHerdrFix, scope, "handoff unsupported"); err != nil {
		t.Fatal(err)
	}
	if err := SetRegistrationError(CodeHerdrCompatibility, scope, "forwarding unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := SetRegistrationError("", scope, ""); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil || len(status.Errors) != 1 {
		t.Fatalf("registration cleared repair failure: %+v %v", status, err)
	}
	for _, entry := range status.Errors {
		if entry.Code != CodeHerdrFix {
			t.Fatalf("wrong remaining failure: %+v", entry)
		}
	}
	if err := ClearError(CodeHerdrFix, scope); err != nil {
		t.Fatal(err)
	}
	status, err = ReadStatusStore()
	if err != nil || len(status.Errors) != 0 {
		t.Fatalf("explicit repair recovery not saved: %+v %v", status, err)
	}
}
