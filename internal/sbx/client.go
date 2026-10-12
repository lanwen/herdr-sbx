// Package sbx owns Docker Sandboxes CLI and daemon communication.
package sbx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

// Sandbox is one daemon inventory record.
type Sandbox struct {
	ID        string
	Name      string
	Status    string
	Workspace string
}

// Daemon describes the status command's observable daemon fields.
type Daemon struct {
	Status string
	Socket string
}

// Authentication is the result of the Authentication diagnostic.
type Authentication string

const (
	AuthenticationPass    Authentication = "pass"
	AuthenticationFail    Authentication = "fail"
	AuthenticationUnknown Authentication = "unknown"
)

// AuthenticationDiagnostic distinguishes an unavailable check from an
// available check whose status is unknown.
type AuthenticationDiagnostic struct {
	Status    Authentication
	Available bool
}

// Client communicates with the sbx CLI and the daemon HTTP socket.
type Client struct {
	runner    process.Runner
	transport http.RoundTripper
}

var ErrInvalidDaemonStatus = errors.New("unrecognised daemon status response")

// CLIError retains combined command output while preserving the underlying
// process error for errors.Is and errors.As callers.
type CLIError struct {
	Err    error
	Output string
}

func (e *CLIError) Error() string { return e.Err.Error() }
func (e *CLIError) Unwrap() error { return e.Err }

// CommandOutput returns output retained by a typed CLI operation error.
func CommandOutput(err error) string {
	var commandErr *CLIError
	if errors.As(err, &commandErr) {
		return commandErr.Output
	}
	return ""
}

// New creates an sbx client. The zero-value transport uses a short-lived Unix
// socket connection for each daemon request.
func New(runner process.Runner) *Client { return &Client{runner: runner} }

// NewWithTransport creates a client using an alternate daemon HTTP transport.
func NewWithTransport(runner process.Runner, transport http.RoundTripper) *Client {
	return &Client{runner: runner, transport: transport}
}

// Version returns the installed Docker Sandboxes version output.
func (c *Client) Version(ctx context.Context) (string, error) {
	output, err := c.cli(ctx, 5*time.Second, false, "--version")
	if err == nil {
		return output, nil
	}
	return c.cli(ctx, 5*time.Second, false, "version")
}

// DaemonStatus reads the JSON status and resolves its reported socket.
func (c *Client) DaemonStatus(ctx context.Context) (Daemon, error) {
	output, err := c.cli(ctx, 5*time.Second, true, "daemon", "status", "--json")
	if err != nil {
		return Daemon{}, &CLIError{Err: err, Output: output}
	}
	var value map[string]any
	if err := decodeJSON(output, &value); err != nil {
		return Daemon{}, fmt.Errorf("%w: %v", ErrInvalidDaemonStatus, err)
	}
	if result, ok := value["result"].(map[string]any); ok {
		value = result
	}
	return Daemon{Status: text(value["status"]), Socket: text(value["socket"])}, nil
}

// JSONCommandsAvailable checks the two JSON commands needed by the plugin.
func (c *Client) JSONCommandsAvailable(ctx context.Context) bool {
	daemonHelp, daemonErr := c.cli(ctx, 5*time.Second, false, "daemon", "status", "--help")
	listHelp, listErr := c.cli(ctx, 5*time.Second, false, "ls", "--help")
	return daemonErr == nil && listErr == nil && strings.Contains(daemonHelp, "--json") && strings.Contains(listHelp, "--json")
}

// DiagnoseAuthentication inspects only the Authentication diagnostic result.
func (c *Client) DiagnoseAuthentication(ctx context.Context) AuthenticationDiagnostic {
	output, _ := c.cli(ctx, 35*time.Second, true, "diagnose", "--json")
	var report struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if decodeJSON(output, &report) != nil {
		return AuthenticationDiagnostic{Status: AuthenticationUnknown}
	}
	for _, check := range report.Checks {
		if check.Name == "Authentication" {
			switch check.Status {
			case "pass":
				return AuthenticationDiagnostic{Status: AuthenticationPass, Available: true}
			case "fail":
				return AuthenticationDiagnostic{Status: AuthenticationFail, Available: true}
			default:
				return AuthenticationDiagnostic{Status: AuthenticationUnknown, Available: true}
			}
		}
	}
	return AuthenticationDiagnostic{Status: AuthenticationUnknown}
}

// WorkspaceDirectory asks sbx for a sandbox's physical workspace path.
func (c *Client) WorkspaceDirectory(ctx context.Context, name string) (string, error) {
	return c.cli(ctx, 15*time.Second, true, "exec", name, "--", "pwd", "-P")
}

// SetupSSHInteractive configures sandbox SSH through the user's terminal.
func (c *Client) SetupSSHInteractive(ctx context.Context) error {
	return c.runner.RunInteractive(ctx, "sbx", c.appScopedArgs("setup", "ssh")...)
}

func (c *Client) cli(ctx context.Context, timeout time.Duration, scoped bool, args ...string) (string, error) {
	if scoped {
		args = c.appScopedArgs(args...)
	}
	return c.runner.CombinedOutput(ctx, process.Command{Name: "sbx", Args: args, Timeout: timeout})
}

func (c *Client) appScopedArgs(args ...string) []string {
	if app := strings.TrimSpace(os.Getenv("HERDR_SANDBOXES_SBX_APP_NAME")); app != "" {
		return append([]string{"--app-name", app}, args...)
	}
	return args
}

func decodeJSON(raw string, destination any) error {
	objectStart := strings.Index(raw, "{")
	arrayStart := strings.Index(raw, "[")
	start := objectStart
	if start < 0 || (arrayStart >= 0 && arrayStart < start) {
		start = arrayStart
	}
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
