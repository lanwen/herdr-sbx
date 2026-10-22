package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var savedMachine = regexp.MustCompile(`(?m)^Saved SSH machine ([^[:space:]]+)\. Remote server is ready\.$`)

// Machine is a catalog entry returned by Herdr.
type Machine struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Session string `json:"session"`
	// Enabled is nil when the catalog does not report the native connection flag.
	Enabled *bool `json:"enabled,omitempty"`
}

// ListMachines reads the local machine catalog.
func (c *Client) ListMachines(ctx context.Context) ([]Machine, string, error) {
	output, err := c.command(ctx, 5*time.Second, true, "machine", "list", "--json")
	if err != nil {
		return nil, output, err
	}
	machines, err := parseMachines(output)
	return machines, output, err
}

// AddMachine creates a machine and returns the ID explicitly acknowledged by
// Herdr's output. Callers must still verify that ID by re-reading the catalog.
func (c *Client) AddMachine(ctx context.Context, label, target string) (string, string, error) {
	output, err := c.command(ctx, 30*time.Second, true, "machine", "add", "--label", label, target)
	if err != nil {
		return "", output, err
	}
	match := savedMachine.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) != 2 {
		return "", output, nil
	}
	return match[1], output, nil
}

// RenameMachine changes the label of a previously verified owned profile.
func (c *Client) RenameMachine(ctx context.Context, id, label string) (string, error) {
	return c.command(ctx, 5*time.Second, true, "machine", "rename", id, "--label", label)
}

// RemoveMachine removes the specified catalog profile.
func (c *Client) RemoveMachine(ctx context.Context, id string) (string, error) {
	return c.command(ctx, 15*time.Second, true, "machine", "remove", id)
}

// SetMachineEnabled changes the saved native connection flag without opening SSH.
// Callers must verify the profile identity and re-read the catalog afterward.
// Disabling does not provide a barrier against clients' already-started reconnects.
func (c *Client) SetMachineEnabled(ctx context.Context, id string, enabled bool) (string, error) {
	if id == "" || strings.TrimSpace(id) != id || strings.HasPrefix(id, "-") {
		return "", errors.New("a saved herdr machine ID is required")
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	return c.command(ctx, 5*time.Second, true, "machine", action, id)
}

// MachineStatus checks whether the selected remote machine's server responds.
func (c *Client) MachineStatus(ctx context.Context, id string) (string, error) {
	return c.command(ctx, 15*time.Second, true, "--machine", id, "status", "server")
}

func parseMachines(raw string) ([]Machine, error) {
	var machines []Machine
	if err := decodeJSON(raw, &machines); err != nil {
		return nil, fmt.Errorf("read herdr machine list: %w", err)
	}
	if machines == nil {
		return nil, errors.New("read herdr machine list: expected a list")
	}
	for _, machine := range machines {
		if strings.TrimSpace(machine.ID) == "" {
			return nil, errors.New("read herdr machine list: entry is missing its ID")
		}
	}
	return machines, nil
}

func decodeJSON(raw string, destination any) error {
	start := strings.IndexAny(raw, "{[")
	if start < 0 {
		return errors.New("no JSON value")
	}
	return json.NewDecoder(strings.NewReader(raw[start:])).Decode(destination)
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
