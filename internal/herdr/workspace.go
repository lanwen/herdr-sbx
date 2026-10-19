package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Pane is the verified workspace location reported by Herdr.
type Pane struct {
	WorkspaceID string `json:"workspace_id"`
	CWD         string `json:"cwd"`
}

// WorkspaceResult is the returned data needed to verify a workspace create.
type WorkspaceResult struct {
	ID       string
	CWD      string
	Rejected string
}

// Panes lists panes for the selected machine.
func (c *Client) Panes(ctx context.Context, machineID string) ([]Pane, string, error) {
	output, err := c.command(ctx, 15*time.Second, true, "--machine", machineID, "pane", "list")
	if err != nil {
		return nil, output, err
	}
	panes, err := parsePanes(output)
	return panes, output, err
}

func parsePanes(output string) ([]Pane, error) {
	var response struct {
		Result struct {
			Panes []Pane `json:"panes"`
		} `json:"result"`
	}
	if err := decodeJSON(output, &response); err != nil {
		return nil, err
	}
	if response.Result.Panes == nil {
		return nil, errors.New("herdr pane list did not report panes")
	}
	for _, pane := range response.Result.Panes {
		if pane.WorkspaceID == "" {
			return nil, errors.New("herdr pane list entry is missing its workspace ID")
		}
	}
	return response.Result.Panes, nil
}

// CreateWorkspace asks Herdr to create a labeled project workspace. The
// caller verifies the returned path and records pending ownership.
func (c *Client) CreateWorkspace(ctx context.Context, machineID, cwd, label string) (WorkspaceResult, string, error) {
	output, err := c.command(ctx, 30*time.Second, true, "--machine", machineID, "workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	var response struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
			RootPane struct {
				CWD string `json:"cwd"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if decodeErr := decodeJSON(output, &response); decodeErr != nil {
		if err != nil {
			return WorkspaceResult{}, output, err
		}
		return WorkspaceResult{}, output, fmt.Errorf("decode workspace result: %w", decodeErr)
	}
	result := WorkspaceResult{ID: response.Result.Workspace.ID, CWD: response.Result.RootPane.CWD}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		result.Rejected = string(response.Error)
		var upstream *CommandError
		if errors.As(err, &upstream) {
			result.Rejected = upstream.Error()
		}
	}
	return result, output, err
}
