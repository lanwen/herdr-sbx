package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveDefaultsAndSandboxOverrides(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	on, off := true, false
	require.NoError(t, SetGuestPluginDefault("example.plugin", true))
	require.NoError(t, SetSandboxPluginOverride("scope", "one", "example.plugin", &off))
	require.NoError(t, SetSandboxPluginOverride("scope", "two", "example.plugin", &on))
	require.NoError(t, SetGuestPluginDefault("example.plugin", false))
	settings, err := ReadGuestPluginDefaults()
	require.NoError(t, err)
	require.False(t, settings.Effective("scope", "one").Plugins["example.plugin"].Enabled)
	require.True(t, settings.Effective("scope", "two").Plugins["example.plugin"].Enabled)
	require.False(t, settings.Effective("scope", "three").Plugins["example.plugin"].Enabled)
	require.NoError(t, SetSandboxPluginOverride("scope", "two", "example.plugin", nil))
	settings, err = ReadGuestPluginDefaults()
	require.NoError(t, err)
	require.False(t, settings.Effective("scope", "two").Plugins["example.plugin"].Enabled, "inherit did not restore current default")
}

func TestErrorsDeduplicateAndRecoveryResolves(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for range 2 {
		require.NoError(t, RecordError(CodeRename, "demo", "conflict"))
	}
	require.NoError(t, RecordError(CodeRename, "other", "conflict"))
	status, err := ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 2, "errors=%+v", status.Errors)
	for _, entry := range status.Errors {
		require.False(t, entry.Scope == "demo" && (entry.Count != 2 || entry.FirstSeen.IsZero() || entry.LastSeen.Before(entry.FirstSeen)), "entry=%+v", entry)
	}
	require.NoError(t, ClearError(CodeRename, "demo"))
	status, err = ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, status.Errors, 1)
}

func TestEnabledChoiceAndControlGenerationPersist(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, SetEnabled("scope", "sandbox", Disabled))
	require.NoError(t, SetWorkerControl(true))
	first, err := ReadStatusStore()
	require.NoError(t, err)
	require.Equal(t, EnabledChoice(Disabled), first.EnabledChoices[StatusKey("scope", "sandbox")], "disabled choice lost")
	require.NoError(t, SetWorkerControl(true))
	applied, err := SetWorkerControlIfGeneration(false, first.WorkerControl.Generation)
	require.NoError(t, err)
	require.False(t, applied)
	final, err := ReadStatusStore()
	require.NoError(t, err)
	require.True(t, final.WorkerControl.Stopped)
	require.False(t, final.WorkerControl.Generation <= first.WorkerControl.Generation)
}
