package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestTerminalWriterSanitizesAndReportsWrites(t *testing.T) {
	var out bytes.Buffer
	input := []byte("café\x1b[31m fixture\r\b\a\n\tsecond line")
	n, err := (terminalWriter{out: &out}).Write(input)
	require.False(t, n != len(input))
	require.NoError(t, err)
	require.EqualValues(t, "café fixture\n\tsecond line", out.String())
	failure := errors.New("fixture writer failed")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"short", nil, io.ErrShortWrite},
		{"error", failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			n, err := (terminalWriter{out: shortTerminalWriter{err: test.err}}).Write(input)
			require.EqualValues(t, 0, n)
			require.ErrorIs(t, err, test.want)
		})
	}
}

type shortTerminalWriter struct{ err error }

func (w shortTerminalWriter) Write(p []byte) (int, error) {
	return len(p) / 2, w.err
}

func TestTerminalTextKeepsReadableMessagesWithoutTerminalControls(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{"color", "before\x1b[31mred\x1b[0m after", "beforered after"},
		{"cursor", "before\x1b[2Jafter", "beforeafter"},
		{"osc", "before\x1b]0;fixture title\aafter", "beforeafter"},
		{"hyperlink", "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\", "label"},
		{"controls", "a\r\b\a\x00\x7f\u0085b", "ab"},
		{"multiline unicode", "sbx · café → ready\n\tsecond line", "sbx · café → ready\n\tsecond line"},
	} {
		t.Run(test.name, func(t *testing.T) {
			{
				got := terminalMessage(test.input)
				require.Equal(t, test.want, got, "message = %q, want %q", got, test.want)
			}
			wantField := strings.NewReplacer("\n", " ", "\t", " ").Replace(test.want)
			{
				got := terminalField(test.input)
				require.Equal(t, wantField, got, "field = %q, want %q", got, wantField)
			}
		})
	}
}

func TestHumanStatusSanitizesSavedFieldsAndPreservesJSON(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	field := "café\x1b[31m fixture\x1b[0m\r\b\u0085\tcolumn\nline"
	detail := "first\x1b]0;fixture title\a line\n\tsecond\r\b line"
	require.NoError(t, store.UpdateStatusStore(func(saved *store.StatusStore) {
		saved.Sandboxes["fixture"] = store.SandboxStatus{
			Name: field, Label: field, Connection: field, Observed: field,
			Detail: detail, AssociationIssue: detail, Enabled: store.Enabled,
		}
		saved.Errors["fixture"] = store.ErrorRecord{Code: "D9999\x1b[31m", Scope: field, Detail: detail}
	}))
	for _, print := range []func(*bytes.Buffer, bool) error{
		func(out *bytes.Buffer, asJSON bool) error { return printStatus(out, asJSON) },
		func(out *bytes.Buffer, asJSON bool) error { return printDaemonStatus(out, asJSON) },
	} {
		var human, structured bytes.Buffer
		require.NoError(t, print(&human, false))
		require.False(t, strings.ContainsAny(human.String(), "\x1b\r\b\u0085"))
		require.Contains(t, human.String(), "café fixture column line")
		require.Contains(t, human.String(), "first line\n\tsecond line")
		require.NoError(t, print(&structured, true))
		var report statusReport
		require.NoError(t, json.Unmarshal(structured.Bytes(), &report))
		require.Len(t, report.Errors, 1)
		require.False(t, report.Errors[0].Scope != field)
		require.False(t, report.Errors[0].Detail != detail)
		require.EqualValues(t, "D9999\x1b[31m", report.Errors[0].Code)
		if len(report.Sandboxes) != 0 {
			row := report.Sandboxes[0]
			require.False(t, row.Name != field)
			require.False(t, row.Label != field)
			require.False(t, row.Connection != field)
			require.False(t, row.Observed != field)
			require.False(t, row.Detail != detail)
			require.False(t, row.AssociationIssue != detail)
			for _, line := range strings.Split(human.String(), "\n") {
				require.False(t, strings.Contains(line, "Enabled") && strings.Contains(line, "café") && strings.Count(line, "café fixture column line") != 4, "sandbox fields reshaped table: %q", line)
			}
		}
	}
}

func TestDaemonSummarySanitizesBeforeApplyingStyles(t *testing.T) {
	var out bytes.Buffer
	paint := func(value, code string) string { return "\x1b[" + code + "m" + value + "\x1b[0m" }
	printDaemonSummary(&out, daemonStatusReport{Daemon: store.WorkerStatus{
		State: "running\x1b[2J", Detail: "first\x1b[31m line\n\tsecond\r line",
	}}, paint)
	require.NotContains(t, out.String(), "\x1b[2J")
	require.NotContains(t, out.String(), "\x1b[31m")
	require.NotContains(t, out.String(), "\r")
	require.Contains(t, out.String(), "\x1b[1mherdr-sbx\x1b[0m")
	require.Contains(t, out.String(), "first line\n\tsecond line")
}
