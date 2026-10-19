package sbx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// NetworkPolicyLog is one blocked destination reported by sbx policy log.
type NetworkPolicyLog struct {
	SandboxID string    `json:"sandbox_id"`
	Host      string    `json:"host"`
	Reason    string    `json:"reason"`
	Count     int       `json:"count"`
	LastSeen  time.Time `json:"last_seen"`
}

// BlockedNetworkRequests reads a bounded, sandbox-scoped policy log snapshot.
// An unavailable or malformed log is an unknown observation, not evidence of
// a policy block.
func (c *Client) BlockedNetworkRequests(ctx context.Context, sandbox string) ([]NetworkPolicyLog, error) {
	if sandbox == "" {
		return nil, errors.New("sandbox name is required for policy log")
	}
	output, err := c.cli(ctx, 5*time.Second, true, "policy", "log", sandbox, "--json", "--type", "network", "--limit", "100")
	if err != nil {
		return nil, &CLIError{Err: err, Output: output}
	}
	var document struct {
		Blocked *[]NetworkPolicyLog `json:"blocked"`
	}
	if err := decodeJSON(output, &document); err != nil || document.Blocked == nil {
		return nil, fmt.Errorf("invalid sandbox policy log response")
	}
	return *document.Blocked, nil
}
