package registration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

const (
	logKeyStage        = "stage"
	logKeySandboxID    = "sandbox_id"
	logKeySandbox      = "sandbox"
	logKeyAttempt      = "attempt"
	logKeyElapsed      = "elapsed_ms"
	logKeyDeadline     = "deadline"
	logKeyAck          = "acknowledged_id"
	logKeyOutput       = "output"
	logKeyErrorType    = "error_type"
	logKeyContextError = "context_error"
	logKeyExitCode     = "exit_code"
)

type registrationIdentityKey struct{}
type registrationIdentity struct {
	Scope   string
	Sandbox sbx.Sandbox
}

type registrationLog struct {
	Stage   string
	Scope   string
	Sandbox sbx.Sandbox
	Attempt int
	Started time.Time
	Timeout time.Duration
	Ack     string
	Err     error
	Output  string
}

// Keep original command evidence separate from the short displayed explanation.
func (s *Service) logRegistration(ctx context.Context, entry registrationLog) {
	output := registrationEvidence(entry.Output)
	deadline := entry.Started.Add(entry.Timeout)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	attributes := []slog.Attr{
		slog.String(logKeyStage, entry.Stage), slog.String(logKeySandboxID, entry.Sandbox.ID), slog.String(logKeySandbox, entry.Sandbox.Name),
		slog.Int(logKeyAttempt, entry.Attempt), slog.Int64(logKeyElapsed, time.Since(entry.Started).Milliseconds()),
		slog.Time(logKeyDeadline, deadline), slog.String(logKeyAck, entry.Ack), slog.String(logKeyOutput, output),
	}
	detail := entry.Stage + " completed"
	if entry.Err != nil {
		message := registrationEvidence(entry.Err.Error())
		attributes = append(attributes, slog.String(logKeyError, message), slog.String(logKeyErrorType, fmt.Sprintf("%T", entry.Err)))
		detail = entry.Stage + " failed: " + message
		output = registrationEvidence(output + "\nCommand error:\n" + message)
	}
	if ctx.Err() != nil {
		attributes = append(attributes, slog.Any(logKeyContextError, ctx.Err()))
	}
	var exit *exec.ExitError
	if errors.As(entry.Err, &exit) {
		attributes = append(attributes, slog.Int(logKeyExitCode, exit.ExitCode()))
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, "Registration operation", attributes...)
	output = registrationEventEvidence(ctx, attributes, output)
	if err := store.RecordEvent(store.EventRecord{Code: store.CodeRegistration, Scope: store.StatusKey(entry.Scope, entry.Sandbox.ID), Name: entry.Sandbox.Name, Detail: detail, Output: output, Resolved: entry.Err == nil, At: time.Now()}); err != nil {
		s.logger.WarnContext(ctx, "Save registration evidence", slog.Any(logKeyError, err))
	}
}

func registrationEventEvidence(ctx context.Context, attributes []slog.Attr, fallback string) string {
	var output bytes.Buffer
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "Registration operation", 0)
	record.AddAttrs(attributes...)
	// The in-memory handler retains the same structured evidence without a terminal sink.
	if err := slog.NewJSONHandler(&output, nil).Handle(ctx, record); err != nil {
		return fallback
	}
	return output.String()
}

func registrationEvidence(output string) string {
	output = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(output))
	output = regexp.MustCompile(`(?im)(authorization:\s*)[^\r\n]*`).ReplaceAllString(output, "${1}<REDACTED>")
	output = regexp.MustCompile(`(?i)((?:token|password|secret|api_key)\s*[:=]\s*)[^\s]+`).ReplaceAllString(output, "${1}<REDACTED>")
	const limit = 16 << 10
	if len(output) > limit {
		output = strings.ToValidUTF8(output[:limit], "�") + fmt.Sprintf("\n[output limited to %d bytes]", limit)
	}
	return output
}
