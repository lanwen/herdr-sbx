// Package process runs bounded operating-system commands. Command meaning and
// argument construction belong to the Herdr and Docker Sandboxes clients.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxOutputBytes = 16 << 20

var errOutputLimit = fmt.Errorf("command output exceeded %d-byte limit", maxOutputBytes)

type cappedOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	if len(p) > maxOutputBytes-b.buffer.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errOutputLimit
	}
	return b.buffer.Write(p)
}

// Command describes one process invocation after its owning client has chosen
// the executable, arguments, timeout, and environment changes.
type Command struct {
	Name    string
	Args    []string
	Timeout time.Duration
	Env     map[string]string
	Unset   []string
	Stdin   io.Reader
}

// Runner executes commands. A custom executor is useful for package tests;
// production uses the operating system implementation.
type Runner struct {
	Execute     func(context.Context, Command) (string, error)
	Interactive func(context.Context, string, ...string) error
}

// CombinedOutput runs a command and returns its combined stdout and stderr.
func (r Runner) CombinedOutput(ctx context.Context, command Command) (string, error) {
	if r.Execute != nil {
		return r.Execute(ctx, command)
	}
	return execute(ctx, command)
}

// RunInteractive attaches the current process streams to a command.
func (r Runner) RunInteractive(ctx context.Context, name string, args ...string) error {
	if r.Interactive != nil {
		return r.Interactive(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func execute(parent context.Context, command Command) (string, error) {
	ctx := parent
	if command.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, command.Timeout)
		defer cancel()
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(runCtx, command.Name, command.Args...)
	cmd.WaitDelay = time.Second
	output := &cappedOutput{cancel: stop}
	// Sharing the writer preserves merged output and serializes writes.
	cmd.Stdout, cmd.Stderr = output, output
	cmd.Stdin = command.Stdin
	if len(command.Env) != 0 || len(command.Unset) != 0 {
		cmd.Env = overlay(os.Environ(), command.Env, command.Unset)
	}
	err := cmd.Run()
	if output.exceeded {
		// Never expose a truncated structured response, even to callers ignoring errors.
		if ctx.Err() == nil {
			return "", errOutputLimit
		}
		output.buffer.Reset()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output.buffer.String(), fmt.Errorf("timed out")
	}
	if err != nil && ctx.Err() != nil {
		return output.buffer.String(), ctx.Err()
	}
	return output.buffer.String(), err
}

func overlay(env []string, values map[string]string, unset []string) []string {
	removed := make(map[string]struct{}, len(values)+len(unset))
	for key := range values {
		removed[key] = struct{}{}
	}
	for _, key := range unset {
		removed[key] = struct{}{}
	}
	result := make([]string, 0, len(env)+len(values))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if _, drop := removed[key]; !drop {
			result = append(result, item)
		}
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
