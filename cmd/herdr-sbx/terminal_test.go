package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestTerminalWriterSanitizesAndReportsWrites(t *testing.T) {
	var out bytes.Buffer
	input := []byte("café\x1b[31m fixture\r\b\a\n\tsecond line")
	n, err := (terminalWriter{out: &out}).Write(input)
	if n != len(input) || err != nil || out.String() != "café fixture\n\tsecond line" {
		t.Fatalf("write = %d, %v; output = %q", n, err, out.String())
	}
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
			if n != 0 || !errors.Is(err, test.want) {
				t.Fatalf("write = %d, %v; want 0, %v", n, err, test.want)
			}
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
			if got := terminalMessage(test.input); got != test.want {
				t.Fatalf("message = %q, want %q", got, test.want)
			}
			wantField := strings.NewReplacer("\n", " ", "\t", " ").Replace(test.want)
			if got := terminalField(test.input); got != wantField {
				t.Fatalf("field = %q, want %q", got, wantField)
			}
		})
	}
}

func TestHumanStatusSanitizesSavedFieldsAndPreservesJSON(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	field := "café\x1b[31m fixture\x1b[0m\r\b\u0085\tcolumn\nline"
	detail := "first\x1b]0;fixture title\a line\n\tsecond\r\b line"
	if err := store.UpdateStatusStore(func(saved *store.StatusStore) {
		saved.Sandboxes["fixture"] = store.SandboxStatus{
			Name: field, Label: field, Connection: field, Observed: field,
			Detail: detail, AssociationIssue: detail, Enabled: store.Enabled,
		}
		saved.Errors["fixture"] = store.ErrorRecord{Code: "D9999\x1b[31m", Scope: field, Detail: detail}
	}); err != nil {
		t.Fatal(err)
	}
	for _, print := range []func(*bytes.Buffer, bool) error{
		func(out *bytes.Buffer, asJSON bool) error { return printStatus(out, asJSON) },
		func(out *bytes.Buffer, asJSON bool) error { return printDaemonStatus(out, asJSON) },
	} {
		var human, structured bytes.Buffer
		if err := print(&human, false); err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(human.String(), "\x1b\r\b\u0085") || !strings.Contains(human.String(), "café fixture column line") || !strings.Contains(human.String(), "first line\n\tsecond line") {
			t.Fatalf("unsafe or unreadable human output: %q", human.String())
		}
		if err := print(&structured, true); err != nil {
			t.Fatal(err)
		}
		var report statusReport
		if err := json.Unmarshal(structured.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Errors) != 1 || report.Errors[0].Scope != field || report.Errors[0].Detail != detail || report.Errors[0].Code != "D9999\x1b[31m" {
			t.Fatalf("JSON diagnostic changed: %+v", report.Errors)
		}
		if len(report.Sandboxes) != 0 {
			row := report.Sandboxes[0]
			if row.Name != field || row.Label != field || row.Connection != field || row.Observed != field || row.Detail != detail || row.AssociationIssue != detail {
				t.Fatalf("JSON sandbox fields changed: %+v", row)
			}
			for _, line := range strings.Split(human.String(), "\n") {
				if strings.Contains(line, "Enabled") && strings.Contains(line, "café") && strings.Count(line, "café fixture column line") != 4 {
					t.Fatalf("sandbox fields reshaped table: %q", line)
				}
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
	if strings.Contains(out.String(), "\x1b[2J") || strings.Contains(out.String(), "\x1b[31m") || strings.Contains(out.String(), "\r") || !strings.Contains(out.String(), "\x1b[1mherdr-sbx\x1b[0m") || !strings.Contains(out.String(), "first line\n\tsecond line") {
		t.Fatalf("unsafe output or intentional styling lost: %q", out.String())
	}
}
