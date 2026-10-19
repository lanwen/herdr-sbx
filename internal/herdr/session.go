package herdr

import (
	"context"
	"errors"
	"os"
	"time"
)

// LocalServerRunning checks the currently bound local server. Workers retain
// their socket override here; only discovery queries use the unbound session.
func (c *Client) LocalServerRunning(ctx context.Context) bool {
	output, err := c.command(ctx, 5*time.Second, true, "status", "server", "--json")
	if err != nil {
		return false
	}
	var status map[string]any
	if decodeJSON(output, &status) != nil {
		return false
	}
	if running, ok := status["running"].(bool); ok {
		return running
	}
	return false
}

// PluginEnabled checks the configured plugin using a clean local session.
func (c *Client) PluginEnabled(ctx context.Context) bool {
	plugin := os.Getenv("HERDR_PLUGIN_ID")
	if plugin == "" {
		plugin = "lanwen.herdr-sbx"
	}
	output, err := c.command(ctx, 5*time.Second, false, "plugin", "list", "--plugin", plugin, "--json")
	if err != nil {
		return false
	}
	items, err := parsePlugins(output)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.ID != plugin {
			continue
		}
		return item.Enabled
	}
	return false
}

// RunningSessionCount counts Herdr sessions that report as running.
func (c *Client) RunningSessionCount(ctx context.Context) (int, error) {
	items, err := c.sessions(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if running, ok := item["running"].(bool); ok && running {
			count++
		}
	}
	return count, nil
}

// RebindToRunningSession selects the socket from a currently running session.
func (c *Client) RebindToRunningSession(ctx context.Context) bool {
	items, err := c.sessions(ctx)
	if err != nil {
		return false
	}
	for _, item := range items {
		running, _ := item["running"].(bool)
		socket := text(item["socket_path"])
		if running && socket != "" {
			c.RebindSocket(socket)
			return true
		}
	}
	return false
}

func (c *Client) sessions(ctx context.Context) ([]map[string]any, error) {
	output, err := c.command(ctx, 5*time.Second, false, "session", "list", "--json")
	if err != nil {
		return nil, err
	}
	var response struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := decodeJSON(output, &response); err != nil {
		return nil, err
	}
	if response.Sessions == nil {
		return nil, errors.New("herdr session list did not report sessions")
	}
	for _, session := range response.Sessions {
		if session == nil {
			return nil, errors.New("herdr session list contains a null entry")
		}
	}
	return response.Sessions, nil
}
