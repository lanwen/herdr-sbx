package herdr

import (
	"context"
	"os"
	"strings"
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
	return text(status["status"]) == "running"
}

// PluginEnabled checks the configured plugin using a clean local session.
func (c *Client) PluginEnabled(ctx context.Context) bool {
	plugin := os.Getenv("HERDR_PLUGIN_ID")
	if plugin == "" {
		plugin = "herdr.docker-sandboxes"
	}
	output, err := c.command(ctx, 5*time.Second, false, "plugin", "list", "--plugin", plugin, "--json")
	if err != nil {
		return false
	}
	items, err := objects(output, "plugins")
	if err != nil {
		return false
	}
	for _, item := range items {
		if first(item, "id", "plugin_id") != plugin {
			continue
		}
		if enabled, ok := item["enabled"].(bool); ok {
			return enabled
		}
		return strings.EqualFold(first(item, "status", "state"), "enabled")
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
		socket := first(item, "socket_path", "socket")
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
	return objects(output, "sessions")
}
