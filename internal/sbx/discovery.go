package sbx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

type discoveryOptions struct {
	platform string
	paths    []string
}

func (c *Client) discoveryOptions() discoveryOptions {
	if c.discovery != nil {
		return *c.discovery
	}
	return discoveryOptions{platform: runtime.GOOS, paths: []string{"/opt/homebrew/bin/sbx", "/usr/local/bin/sbx"}}
}

func (c *Client) missingPATHExecutable(err error) bool {
	return c.discoveryOptions().platform == "darwin" && errors.Is(err, exec.ErrNotFound)
}

func (c *Client) homebrewExecutable() (string, error) {
	errs := []error{fmt.Errorf("sbx on PATH: %w", exec.ErrNotFound)}
	for _, candidate := range c.discoveryOptions().paths {
		binary, err := exec.LookPath(candidate)
		if err == nil {
			return binary, nil
		}
		errs = append(errs, err)
	}
	return "", errors.Join(errs...)
}

func (c *Client) runCLI(ctx context.Context, command process.Command) (string, error) {
	output, err := c.runner.CombinedOutput(ctx, command)
	if !c.missingPATHExecutable(err) {
		return output, err
	}
	command.Name, err = c.homebrewExecutable()
	if err != nil {
		return output, err
	}
	return c.runner.CombinedOutput(ctx, command)
}

func (c *Client) discoverDaemon(ctx context.Context) (Daemon, error) {
	if strings.TrimSpace(os.Getenv("HERDR_SANDBOXES_SBX_APP_NAME")) == "" {
		daemon, err := c.defaultDaemon(ctx)
		if err == nil {
			return daemon, nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return Daemon{}, err
		}
	}
	binary, err := c.homebrewExecutable()
	if err != nil {
		return Daemon{}, err
	}
	output, err := c.runner.CombinedOutput(ctx, process.Command{Name: binary, Args: c.appScopedArgs("daemon", "status", "--json"), Timeout: 5 * time.Second})
	if err != nil {
		return Daemon{}, &CLIError{Err: err, Output: output}
	}
	return parseDaemonStatus(output)
}

func (c *Client) defaultDaemon(ctx context.Context) (Daemon, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Daemon{}, err
	}
	socket := filepath.Join(home, "Library", "Application Support", "com.docker.sandboxes", "sandboxes", "sandboxd", "sandboxd.sock")
	if _, err := c.Inventory(ctx, socket); err != nil {
		return Daemon{}, fmt.Errorf("probe sandbox daemon at %s: %w", socket, err)
	}
	return Daemon{Status: "running", Socket: socket}, nil
}
