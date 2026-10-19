package herdr

import (
	"context"
	"fmt"
	"strings"
)

// HandoffGuest asks the running guest to transfer its panes to the selected
// executable. There is deliberately no stop/restart fallback on failure.
func (c *Client) HandoffGuest(ctx context.Context, target, version string) error {
	script := guestExecutable + `"$h" --session default server live-handoff --import-exe "$h" --expected-version ` + quoteShell(version)
	output, err := c.guestCommand(ctx, target, script)
	if err != nil {
		return fmt.Errorf("guest live handoff: %w: %s", err, strings.TrimSpace(output))
	}
	running, actual, err := c.GuestServerVersion(ctx, target)
	if err != nil {
		return err
	}
	if !running || actual != version {
		return fmt.Errorf("guest server did not confirm Herdr %s after live handoff", version)
	}
	return nil
}

// GuestServerVersion reads server state without starting or stopping it.
func (c *Client) GuestServerVersion(ctx context.Context, target string) (bool, string, error) {
	output, err := c.guestCommand(ctx, target, guestExecutable+`"$h" --session default status server --json`)
	if err != nil {
		return false, "", fmt.Errorf("inspect guest server: %w: %s", err, strings.TrimSpace(output))
	}
	var response struct {
		Running *bool  `json:"running"`
		Version string `json:"version"`
	}
	if err := decodeJSON(output, &response); err != nil {
		return false, "", err
	}
	if response.Running == nil {
		return false, "", fmt.Errorf("guest server did not report whether it is running")
	}
	if *response.Running && response.Version == "" {
		return false, "", fmt.Errorf("guest server did not report its version")
	}
	return *response.Running, response.Version, nil
}
