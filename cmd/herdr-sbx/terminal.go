package main

import (
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// terminalMessage removes guest-supplied terminal commands while retaining
// readable Unicode and the line breaks and indentation used by diagnostics.
func terminalMessage(value string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

// terminalField cannot introduce a new row or column into a status table.
func terminalField(value string) string {
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(terminalMessage(value))
}

// terminalWriter sanitizes hidden worker diagnostics when written to a terminal
// or a log, without changing saved observations or structured output.
type terminalWriter struct{ out io.Writer }

func (w terminalWriter) Write(p []byte) (int, error) {
	text := terminalMessage(string(p))
	n, err := io.WriteString(w.out, text)
	if err != nil {
		return 0, err
	}
	if n != len(text) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
