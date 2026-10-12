package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Pane is the verified workspace location reported by Herdr.
type Pane struct {
	WorkspaceID string
	CWD         string
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
	items, err := objects(output, "panes")
	if err != nil {
		return nil, err
	}
	panes := make([]Pane, 0, len(items))
	for _, item := range items {
		panes = append(panes, Pane{WorkspaceID: text(item["workspace_id"]), CWD: rawText(item["cwd"])})
	}
	return panes, nil
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
	}
	return result, output, err
}

func rawText(value any) string {
	text, _ := value.(string)
	return text
}
