package store

import (
	"testing"
)

func TestGuestDefaultsAreExplicitAndIndependent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	settings, err := ReadGuestPluginDefaults()
	if err != nil || len(settings.Plugins) != 0 {
		t.Fatalf("fresh defaults: %+v %v", settings, err)
	}
	if err := SetGuestPluginDefault("one.plugin", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetGuestPluginDefault("two.plugin", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetGuestPluginDefault("one.plugin", false, nil); err != nil {
		t.Fatal(err)
	}
	settings, err = ReadGuestPluginDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Plugins["one.plugin"].Enabled || !settings.Plugins["two.plugin"].Enabled || settings.Plugins["new.plugin"].Enabled {
		t.Fatal(settings)
	}
	status, err := ReadStatusStore()
	if err != nil || status.ReviewrEnabled {
		t.Fatalf("defaults changed telemetry: %v", err)
	}
}
func TestMigrateExplicitReviewrOptIn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := UpdateStatusStore(func(s *StatusStore) { s.ReviewrEnabled = true }); err != nil {
		t.Fatal(err)
	}
	settings, err := ReadGuestPluginDefaults()
	if err != nil || !settings.Plugins["persiyanov.reviewr"].Enabled {
		t.Fatal(settings, err)
	}
	if err := SetGuestPluginDefault("persiyanov.reviewr", false, nil); err != nil {
		t.Fatal(err)
	}
	settings, err = ReadGuestPluginDefaults()
	if err != nil || settings.Plugins["persiyanov.reviewr"].Enabled {
		t.Fatal("legacy state overrode new opt-out", err)
	}
}
