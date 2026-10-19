package store

import "testing"

func TestGuestDefaultsAreExplicitAndIndependent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	settings, err := ReadGuestPluginDefaults()
	if err != nil || len(settings.Plugins) != 0 {
		t.Fatalf("fresh defaults: %+v %v", settings, err)
	}
	if err := SetGuestPluginDefault("one.plugin", true); err != nil {
		t.Fatal(err)
	}
	if err := SetGuestPluginDefault("two.plugin", true); err != nil {
		t.Fatal(err)
	}
	if err := SetGuestPluginDefault("one.plugin", false); err != nil {
		t.Fatal(err)
	}
	settings, err = ReadGuestPluginDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Plugins["one.plugin"].Enabled || !settings.Plugins["two.plugin"].Enabled || settings.Plugins["new.plugin"].Enabled {
		t.Fatal(settings)
	}
}
