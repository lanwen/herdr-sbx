package panel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func (s *Service) chooseSandbox(reader *bufio.Reader, out io.Writer, runningOnly bool) (precheck.Result, sbx.Sandbox, error) {
	p := s.registration.Inspect()
	if p.Socket == "" || !hasLevel(p, "Sandbox daemon", precheck.Ready) {
		return p, sbx.Sandbox{}, errors.New("sandbox selection needs a running daemon")
	}
	items, err := s.registration.SandboxInventory(context.Background(), p.Socket)
	if err != nil {
		return p, sbx.Sandbox{}, err
	}
	choices := make([]sbx.Sandbox, 0, len(items))
	for _, sandbox := range items {
		if sandbox.ID != "" && sandbox.Name != "" && (!runningOnly || strings.EqualFold(sandbox.Status, "running")) {
			choices = append(choices, sandbox)
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Name < choices[j].Name })
	if len(choices) == 0 {
		return p, sbx.Sandbox{}, errors.New("no eligible sandboxes found")
	}
	fmt.Fprintln(out, "Select a sandbox:")
	for i, sandbox := range choices {
		fmt.Fprintf(out, "  %d  %s (%s)\n", i+1, sandbox.Name, sandbox.Status)
	}
	for {
		fmt.Fprint(out, "Number (Enter to cancel): ")
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			return p, sbx.Sandbox{}, io.EOF
		}
		index, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && index > 0 && index <= len(choices) {
			return p, choices[index-1], nil
		}
		fmt.Fprintln(out, "Choose a number from the list.")
	}
}

func (s *Service) temporaryInstall(reader *bufio.Reader, out io.Writer) {
	p, selected, err := s.chooseSandbox(reader, out, true)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			fmt.Fprintln(out, err)
		}
		return
	}
	fmt.Fprintf(out, "Temporarily allow HTTPS GET downloads for %s (ID %s):\n", selected.Name, selected.ID)
	for _, access := range s.registration.InstallationAccess() {
		fmt.Fprintf(out, "  %s%s\n", access.Host, access.Path)
	}
	fmt.Fprintln(out, "All processes in this sandbox share this access. A crash can leave rules active until the next plugin activation. Existing rules and denies are preserved.")
	fmt.Fprint(out, "Type allow to approve this attempt: ")
	approval, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(approval) != "allow" {
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = s.registration.ReconcileAuthorized(ctx, out, p.Scope, p.Socket, selected.ID, selected.Name)
	if err != nil {
		fmt.Fprintln(out, "Installation failed:", err)
	}
	waitForReturn(reader, out)
}

func hasLevel(result precheck.Result, name string, expected precheck.Level) bool {
	for _, check := range result.Checks {
		if check.Name == name && check.Level == expected {
			return true
		}
	}
	return false
}
