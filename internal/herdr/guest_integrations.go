package herdr

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PrepareGuestIntegrations installs agent hooks for supported guest kits.
// beforeInstall, when supplied, rechecks eligibility after discovery.
func (c *Client) PrepareGuestIntegrations(ctx context.Context, target string, beforeInstall func() error) error {
	output, err := c.guestCommand(ctx, target, guestExecutable+`"$h" api schema --json`)
	if err != nil {
		return fmt.Errorf("discover guest integrations: %w: %s", err, strings.TrimSpace(output))
	}
	targets, err := integrationTargets(output)
	if err != nil {
		return fmt.Errorf("discover guest integrations: %w", err)
	}
	if beforeInstall != nil {
		if err := beforeInstall(); err != nil {
			return err
		}
	}
	script := guestExecutable + "for target in " + strings.Join(targets, " ") + `; do
 executable=$(command -v "$target" 2>/dev/null || true)
 case "$executable" in */*) ;; *) executable= ;; esac
 if [ -d "/usr/share/sandbox/kit/$target" ] || { [ -f "$executable" ] && [ -x "$executable" ]; }; then
  "$h" integration install "$target"
 fi
done`
	output, err = c.guestCommand(ctx, target, script)
	if err != nil {
		return fmt.Errorf("install guest integrations: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func integrationTargets(output string) ([]string, error) {
	var schema struct {
		Schemas struct {
			Request struct {
				Defs struct {
					IntegrationTarget struct {
						Enum []string `json:"enum"`
					} `json:"IntegrationTarget"`
				} `json:"$defs"`
			} `json:"request"`
		} `json:"schemas"`
	}
	if err := decodeJSON(output, &schema); err != nil {
		return nil, fmt.Errorf("invalid integration target schema: %w", err)
	}
	targets := schema.Schemas.Request.Defs.IntegrationTarget.Enum
	if len(targets) == 0 {
		return nil, errors.New("guest schema has no integration targets")
	}
	for i, target := range targets {
		if target == "" || target[0] < 'a' || target[0] > 'z' || strings.Trim(target, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
			return nil, errors.New("guest schema has an unsafe integration target")
		}
		targets[i] = quoteShell(target)
	}
	return targets, nil
}
