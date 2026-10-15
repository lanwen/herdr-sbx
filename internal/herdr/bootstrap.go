package herdr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// GuestServerRunning checks the default guest session before machine add can
// bootstrap it from the SSH login directory.
func (c *Client) GuestServerRunning(ctx context.Context, target string) (bool, error) {
	output, err := c.guestCommand(ctx, target, guestExecutable+`"$h" --session default status server --json`)
	if err != nil {
		return false, fmt.Errorf("inspect guest server: %w: %s", err, strings.TrimSpace(output))
	}
	var response struct {
		Running *bool `json:"running"`
	}
	if err := decodeJSON(output, &response); err != nil {
		return false, err
	}
	if response.Running != nil {
		return *response.Running, nil
	}
	return false, fmt.Errorf("guest server status did not report whether it is running")
}

const guestExecutable = `set -eu; if command -v herdr >/dev/null 2>&1; then h=$(command -v herdr); else h="$HOME/.local/bin/herdr"; fi; `

// StartProjectServer seeds the first workspace before machine add attaches.
// Existing/restored sessions are never cleared. Verify the resulting pane so
// unexpected startup behavior cannot silently add a home workspace followed
// by a second project workspace.
func (c *Client) StartProjectServer(ctx context.Context, target, cwd string) error {
	script := guestExecutable + `HERDR_STARTUP_CWD=` + quoteShell(cwd) + ` nohup "$h" --session default server </dev/null >/dev/null 2>&1 &
for attempt in 1 2 3 4 5 6 7 8 9 10; do
 if output=$("$h" --session default pane list 2>/dev/null); then printf '%s\n' "$output"; exit 0; fi
 sleep 1
done
printf '%s\n' 'guest server did not become ready' >&2
exit 1`
	output, err := c.guestCommand(ctx, target, script)
	if err != nil {
		return fmt.Errorf("start project server: %w: %s", err, strings.TrimSpace(output))
	}
	panes, err := parsePanes(output)
	if err != nil {
		return err
	}
	for _, pane := range panes {
		if pane.WorkspaceID != "" && pane.CWD == cwd {
			return nil
		}
	}
	return fmt.Errorf("guest server did not create the requested project workspace; update guest Herdr or inspect the existing session")
}

func (c *Client) guestCommand(ctx context.Context, target, script string) (string, error) {
	return c.runner.CombinedOutput(ctx, process.Command{Name: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, script}, Timeout: 15 * time.Second})
}

func quoteShell(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
