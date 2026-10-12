package guestplugins

import (
	"context"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestCatalogDiscoveryDoesNotOptIn(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
		return `[{"plugin_id":"old.plugin","enabled":false},{"plugin_id":"new.plugin","enabled":true},{"plugin_id":"lanwen.herdr-sbx","enabled":true}]`, nil
	}})
	if err := store.SetGuestPluginDefault("old.plugin", true, nil); err != nil {
		t.Fatal(err)
	}
	choices, err := Choices(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || choices[0].ID != "new.plugin" || choices[0].Enabled || !choices[1].Enabled {
		t.Fatalf("choices: %+v", choices)
	}
	settings, err := store.ReadGuestPluginDefaults()
	if err != nil || len(settings.Plugins) != 1 {
		t.Fatal("discovery wrote defaults", err)
	}
	if err := store.SetGuestPluginDefault("old.plugin", false, nil); err != nil {
		t.Fatal(err)
	}
	noCommands := herdr.New(process.Runner{Execute: func(context.Context, process.Command) (string, error) {
		t.Fatal("opt-out contacted a guest or local Herdr")
		return "", nil
	}})
	if err := Apply(context.Background(), noCommands, nil, "unused", nil); err != nil {
		t.Fatal(err)
	}
}
