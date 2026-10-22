package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGuestDefaultsAreExplicitAndIndependent(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	settings, err := ReadGuestPluginDefaults()
	require.NoError(t, err)
	require.Len(t, settings.Plugins, 0)
	require.NoError(t, SetGuestPluginDefault("one.plugin", true))
	require.NoError(t, SetGuestPluginDefault("two.plugin", true))
	require.NoError(t, SetGuestPluginDefault("one.plugin", false))
	settings, err = ReadGuestPluginDefaults()
	require.NoError(t, err)
	require.False(t, settings.Plugins["one.plugin"].Enabled)
	require.True(t, settings.Plugins["two.plugin"].Enabled)
	require.False(t, settings.Plugins["new.plugin"].Enabled)
}
