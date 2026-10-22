package guestplugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/stretchr/testify/require"
)

func TestPreparationStopsBeforeEachGuestConnection(t *testing.T) {
	for _, phase := range []string{"before inspection", "after inspection", "after transfer", "cancelled context"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			t.Setenv("HERDR_CONFIG_PATH", filepath.Join(t.TempDir(), "absent"))
			root := t.TempDir()
			manifest := filepath.Join(root, "herdr-plugin.toml")
			require.NoError(t, os.WriteFile(manifest, []byte("id='example.cancel'\nversion='1.0.0'\n"), 0600))
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
				require.EqualValues(t, "ssh", cmd.Name)
				require.True(t, allowed)
				require.NoError(t, ctx.Err())
				if cmd.Stdin != nil {
					allowed = false
					return "", nil
				}
				if phase == "after inspection" {
					allowed = false
				}
				return guestInspectionResponse(t, []herdr.PluginInstall{}, "external", ""), nil
			}})
			err := prepare(ctx, client, "fixture.sbx", "scope", "sandbox", local, func() bool { return allowed })
			wantCalls := map[string]int{"before inspection": 0, "after inspection": 1, "after transfer": 2, "cancelled context": 0}[phase]
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, calls != wantCalls)
		})
	}
}
