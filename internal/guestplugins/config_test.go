package guestplugins

import (
	"bytes"
	"testing"
)

const hostKeys = `[keys]
prefix = ["ctrl+b", "ctrl+a"]
[[keys.command]]
key = "prefix+ctrl+r"
type = "plugin_action"
command = "persiyanov.reviewr.toggle"
[[keys.command]]
key = "prefix+z"
type = "plugin_action"
command = "other.toggle"
`

func TestMergeKeys(t *testing.T) {
	guest := []byte("# keep a backup of comments\n[theme]\nname = 'sand'\n")
	merged, err := mergeKeys([]byte(hostKeys), guest, reviewrID)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseConfig(merged)
	if err != nil {
		t.Fatal(err)
	}
	if doc["theme"].(map[string]any)["name"] != "sand" {
		t.Fatal("lost guest settings")
	}
	keys := doc["keys"].(map[string]any)
	if len(keys["command"].([]any)) != 1 {
		t.Fatal("copied unrelated shortcuts")
	}
	if len(keyNames(keys["prefix"])) != 2 {
		t.Fatal("missing local prefix")
	}
	again, err := mergeKeys([]byte(hostKeys), merged, reviewrID)
	if err != nil || !bytes.Equal(again, merged) {
		t.Fatalf("not idempotent: %v", err)
	}
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
			if _, err := mergeKeys([]byte(hostKeys), []byte(guest), reviewrID); err == nil {
				t.Fatal("accepted conflict")
			}
		})
	}
}
