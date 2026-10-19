// Package herdr owns Herdr CLI and Herdr-over-SSH communication.
package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
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
		plugin = "lanwen.herdr-sbx"
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
	output, err := c.runner.CombinedOutput(ctx, command)
	return output, parseCommandError(err, output)
}

func commandError(err error, output string) string {
	var upstream *CommandError
	if errors.As(err, &upstream) {
		return upstream.Error()
	}
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

// CommandError preserves a failure reported by herdr rather than its process wrapper.
// Code is the upstream code, when the CLI supplied a structured error response.
type CommandError struct {
	Code    string
	Message string
	Err     error
}

func (e *CommandError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("herdr [%s]: %s", e.Code, e.Message)
	}
	return "herdr: " + e.Message
}

func (e *CommandError) Unwrap() error { return e.Err }

// MachineForwardingUnsupported identifies an explicit remote executable
// compatibility rejection. Different version numbers alone do not establish it.
func MachineForwardingUnsupported(err error) bool {
	var upstream *CommandError
	return errors.As(err, &upstream) && strings.Contains(strings.ToLower(upstream.Message), "remote herdr does not support machine api forwarding")
}

var debugCommandError = regexp.MustCompile(`^Error: Custom \{ kind: [A-Za-z_][A-Za-z0-9_]*, error: ("(?:[^"\\]|\\.)*") \}$`)

func parseCommandError(cause error, output string) error {
	var response struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	message := strings.TrimSpace(output)
	if json.Unmarshal([]byte(message), &response) == nil && response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return &CommandError{Code: response.Error.Code, Message: response.Error.Message, Err: cause}
	}
	// Rust's Debug rendering is not JSON. Accept only the known complete wrapper
	// and decode its quoted string; arbitrary output is never a herdr error node.
	if cause != nil {
		if matches := debugCommandError.FindStringSubmatch(message); len(matches) == 2 {
			if detail, err := strconv.Unquote(matches[1]); err == nil {
				return &CommandError{Message: detail, Err: cause}
			}
		}
	}
	return cause
}
