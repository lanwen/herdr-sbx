// Package herdr owns Herdr CLI and Herdr-over-SSH communication.
package herdr

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// MinimumVersion is the oldest local or guest Herdr version this plugin supports.
const MinimumVersion = "0.9.1"

// Client makes typed requests to local and remote Herdr installations.
type Client struct {
	runner process.Runner
	socket atomic.Value
}

// New creates a Herdr client using the supplied process runner.
func New(runner process.Runner) *Client {
	client := &Client{runner: runner}
	client.socket.Store("")
	return client
}

// Executable returns HERDR_BIN_PATH or the default executable name.
func (c *Client) Executable() string {
	if binary := os.Getenv("HERDR_BIN_PATH"); binary != "" {
		return binary
	}
	return "herdr"
}

// Version reads the local Herdr version.
func (c *Client) Version(ctx context.Context) (string, error) {
	return c.command(ctx, 5*time.Second, true, "--version")
}

// OpenPane opens a plugin entrypoint in the requested pane placement.
func (c *Client) OpenPane(ctx context.Context, entrypoint, placement string) error {
	plugin := os.Getenv("HERDR_PLUGIN_ID")
	if plugin == "" {
		plugin = "herdr.docker-sandboxes"
	}
	args := []string{"plugin", "pane", "open", "--plugin", plugin, "--entrypoint", entrypoint, "--placement", placement, "--focus"}
	if placement == "popup" {
		args = append(args, "--width", "110", "--height", "32")
	}
	output, err := c.command(ctx, 10*time.Second, true, args...)
	if err != nil {
		return errors.New(commandError(err, output))
	}
	return nil
}

// Notify shows a Herdr notification.
func (c *Client) Notify(ctx context.Context, title, detail string) error {
	_, err := c.command(ctx, 5*time.Second, true, "notification", "show", title, "--body", detail, "--sound", "none")
	return err
}

// RebindSocket directs bound operations to a verified running Herdr session.
func (c *Client) RebindSocket(socket string) { c.socket.Store(socket) }

// BoundSocket returns the worker's current explicit socket override.
func (c *Client) BoundSocket() string { return c.socket.Load().(string) }

func (c *Client) command(ctx context.Context, timeout time.Duration, bound bool, args ...string) (string, error) {
	command := process.Command{Name: c.Executable(), Args: args, Timeout: timeout}
	if bound {
		if socket := c.BoundSocket(); socket != "" {
			command.Env = map[string]string{"HERDR_SOCKET_PATH": socket}
		}
	} else {
		command.Unset = []string{"HERDR_SOCKET_PATH", "HERDR_CLIENT_SOCKET_PATH"}
	}
	return c.runner.CombinedOutput(ctx, command)
}

func commandError(err error, output string) string {
	message := strings.TrimSpace(output)
	if message == "" {
		return err.Error()
	}
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 180 {
		message = message[:177] + "..."
	}
	return message
}
