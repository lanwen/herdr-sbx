package guestplugins

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
)

func TestPreparationStopsBeforeEachGuestConnection(t *testing.T) {
	for _, phase := range []string{"before inspection", "after inspection", "after transfer", "cancelled context"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
			root := t.TempDir()
			manifest := filepath.Join(root, "herdr-plugin.toml")
			if err := os.WriteFile(manifest, []byte("id='example.cancel'\nversion='1.0.0'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			local := herdr.PluginInstall{ID: "example.cancel", Version: "1.0.0", Root: root, ManifestPath: manifest}
			allowed := phase != "before inspection"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "cancelled context" {
				cancel()
			}
			calls := 0
			client := herdr.New(process.Runner{Execute: func(_ context.Context, cmd process.Command) (string, error) {
				calls++
				if cmd.Name != "ssh" || !allowed || ctx.Err() != nil {
					t.Fatalf("guest connection after permission ended: %#v", cmd)
				}
				if cmd.Stdin != nil {
					allowed = false
					return "", nil
				}
				if phase == "after inspection" {
					allowed = false
				}
				catalog := base64.StdEncoding.EncodeToString([]byte(`{"result":{"plugins":[]}}`))
				return "HERDR_SBX_PLUGIN_V1_BEGIN\nLinux aarch64\n" + base64.StdEncoding.EncodeToString([]byte("/home/guest/config.toml")) + "\nabsent\n\n" + catalog + "\nexternal\n\nHERDR_SBX_PLUGIN_V1_END\n", nil
			}})
			err := prepare(ctx, client, "fixture.sbx", "scope", "sandbox", local, func() bool { return allowed })
			wantCalls := map[string]int{"before inspection": 0, "after inspection": 1, "after transfer": 2, "cancelled context": 0}[phase]
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("calls=%d want=%d err=%v", calls, wantCalls, err)
			}
		})
	}
}
