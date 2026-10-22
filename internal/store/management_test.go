package store

import "testing"

func TestLiveDefaultsAndSandboxOverrides(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	on, off := true, false
	if err := SetGuestPluginDefault("example.plugin", true); err != nil {
		t.Fatal(err)
	}
	if err := SetSandboxPluginOverride("scope", "one", "example.plugin", &off); err != nil {
		t.Fatal(err)
	}
	if err := SetSandboxPluginOverride("scope", "two", "example.plugin", &on); err != nil {
		t.Fatal(err)
	}
	if err := SetGuestPluginDefault("example.plugin", false); err != nil {
		t.Fatal(err)
	}
	settings, err := ReadGuestPluginDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Effective("scope", "one").Plugins["example.plugin"].Enabled || !settings.Effective("scope", "two").Plugins["example.plugin"].Enabled || settings.Effective("scope", "three").Plugins["example.plugin"].Enabled {
		t.Fatalf("overrides not independent: %+v", settings)
	}
	if err := SetSandboxPluginOverride("scope", "two", "example.plugin", nil); err != nil {
		t.Fatal(err)
	}
	settings, err = ReadGuestPluginDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Effective("scope", "two").Plugins["example.plugin"].Enabled {
		t.Fatal("inherit did not restore current default")
	}
}

func TestErrorsDeduplicateAndRecoveryResolves(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for range 2 {
		if err := RecordError(CodeRename, "demo", "conflict"); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecordError(CodeRename, "other", "conflict"); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Errors) != 2 {
		t.Fatalf("errors=%+v", status.Errors)
	}
	for _, entry := range status.Errors {
		if entry.Scope == "demo" && (entry.Count != 2 || entry.FirstSeen.IsZero() || entry.LastSeen.Before(entry.FirstSeen)) {
			t.Fatalf("entry=%+v", entry)
		}
	}
	if err := ClearError(CodeRename, "demo"); err != nil {
		t.Fatal(err)
	}
	status, err = ReadStatusStore()
	if err != nil || len(status.Errors) != 1 {
		t.Fatalf("errors=%+v err=%v", status.Errors, err)
	}
}

func TestEnabledChoiceAndControlGenerationPersist(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := SetEnabled("scope", "sandbox", Disabled); err != nil {
		t.Fatal(err)
	}
	if err := SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	first, err := ReadStatusStore()
	if err != nil {
		t.Fatal(err)
	}
	if first.EnabledChoices[StatusKey("scope", "sandbox")] != Disabled {
		t.Fatal("disabled choice lost")
	}
	if err := SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	applied, err := SetWorkerControlIfGeneration(false, first.WorkerControl.Generation)
	if err != nil || applied {
		t.Fatalf("stale ON overwrote later OFF: %v %v", applied, err)
	}
	final, err := ReadStatusStore()
	if err != nil || !final.WorkerControl.Stopped || final.WorkerControl.Generation <= first.WorkerControl.Generation {
		t.Fatalf("control=%+v err=%v", final.WorkerControl, err)
	}
}
