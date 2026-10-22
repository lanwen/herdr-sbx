package guestplugins

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

const hostKeys = `[keys]
prefix = ["ctrl+b", "ctrl+a"]
[[keys.command]]
key = "prefix+ctrl+r"
type = "plugin_action"
command = "example.actions.toggle"
[[keys.command]]
key = "prefix+z"
type = "plugin_action"
command = "other.toggle"
`

func TestMergeKeys(t *testing.T) {
	guest := []byte("# keep a backup of comments\n[theme]\nname = 'sand'\n")
	merged, err := mergeKeys([]byte(hostKeys), guest, "example.actions")
	require.NoError(t, err)
	doc, err := parseConfig(merged)
	require.NoError(t, err)
	require.False(t, doc["theme"].(map[string]any)["name"] != "sand", "lost guest settings")
	keys := doc["keys"].(map[string]any)
	require.Len(t, keys["command"].([]any), 1, "copied unrelated shortcuts")
	require.Len(t, keyNames(keys["prefix"]), 2, "missing local prefix")
	again, err := mergeKeys([]byte(hostKeys), merged, "example.actions")
	require.NoError(t, err)
	require.True(t, bytes.Equal(again, merged), "merge must be idempotent")
}
func TestMergeRejectsConflicts(t *testing.T) {
	for name, guest := range map[string]string{
		"custom": `[keys]
[[keys.command]]
key="prefix+ctrl+r"
type="plugin_action"
command="other.toggle"`,
		"builtin": `[keys]
quit="prefix+ctrl+r"`,
		"prefix": `[keys]
prefix="ctrl+x"`,
		"prefix collision": `[keys]
quit="ctrl+a"`,
		"invalid": "[broken",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mergeKeys([]byte(hostKeys), []byte(guest), "example.actions")
			require.Error(t, err, "accepted conflict")
		})
	}
}
