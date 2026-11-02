package herdr

import (
	"context"
	"fmt"
	"strings"
)

// PrepareGuestIntegrations installs agent hooks only for existing guest configurations.
func (c *Client) PrepareGuestIntegrations(ctx context.Context, target string) error {
	output, err := c.guestCommand(ctx, target, guestExecutable+guestIntegrationSetup)
	if err != nil {
		return fmt.Errorf("install guest integrations: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

const guestIntegrationSetup = `
if [ -d "${CLAUDE_CONFIG_DIR:-$HOME/.claude}" ]; then
 "$h" integration install claude
fi
if [ -d "${CODEX_HOME:-$HOME/.codex}" ]; then
 "$h" integration install codex
fi`
