package panel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
)

// Service connects user actions to the operation services they invoke.
type Service struct {
	registration *registration.Service
	lifecycle    *lifecycle.Service
	sbx          *sbx.Client
}

// New builds the interactive command surface around shared operation clients.
func New(registrations *registration.Service, lifecycleService *lifecycle.Service, sandboxClient *sbx.Client) *Service {
	registrations.SetCheckPrinter(PrintChecks)
	lifecycleService.SetCheckPrinter(PrintChecks)
	return &Service{registration: registrations, lifecycle: lifecycleService, sbx: sandboxClient}
}

// Run renders the panel until the user closes it or input ends.
func (s *Service) Run(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	firstActivation := true
	for {
		fmt.Fprint(out, "\x1b[2J\x1b[H")
		fmt.Fprintln(out, "lanwen/herdr-sbx")
		fmt.Fprintln(out)
		prerequisites := s.registration.Inspect()
		if firstActivation {
			firstActivation = false
			if err := s.registration.RecoverPendingPolicy(prerequisites.Scope, prerequisites.Socket); err != nil {
				fmt.Fprintln(out, "Temporary access cleanup:", err)
			}
		}
		printWorkerStatus(out)
		PrintChecks(out, prerequisites)
		printSandboxStatuses(out, prerequisites.Scope)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Stopped sandbox registrations remain available for ordinary SSH reconnects.")
		fmt.Fprintln(out, "c  Check again (observation only)")
		fmt.Fprintln(out, "r  Register prepared running sandboxes")
		fmt.Fprintln(out, "a  Adopt an existing sandbox machine")
		fmt.Fprintln(out, "s  Set up sandbox SSH")
		fmt.Fprintln(out, "b  Start background worker")
		fmt.Fprintln(out, "w  Watch sandbox lifecycle")
		fmt.Fprintln(out, "t  Retry Herdr preparation for one sandbox")
		fmt.Fprintln(out, "n  Temporarily allow downloads and install Herdr")
		fmt.Fprintln(out, "g  Select guest plugins (defaults)")
		fmt.Fprintln(out, "q  Close")
		fmt.Fprint(out, "> ")

		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "c", "":
			continue
		case "a":
			s.adoptMachine(reader, out)
		case "r":
			fmt.Fprintln(out, "")
			if err := s.registration.Reconcile(out); err != nil {
				fmt.Fprintln(out, "Reconciliation failed:", err)
			}
			waitForReturn(reader, out)
		case "s":
			s.repair("ssh", reader, out)
		case "b":
			if err := s.lifecycle.StartWorker(); err != nil {
				fmt.Fprintln(out, "Worker start failed:", err)
			} else {
				fmt.Fprintln(out, "Worker start requested. Press c to check its status.")
			}
			waitForReturn(reader, out)
		case "w":
			return s.lifecycle.Watch(out)
		case "t":
			p, selected, err := s.chooseSandbox(reader, out, false)
			if errors.Is(err, io.EOF) {
				continue
			}
			if err == nil {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				err = s.registration.RetrySelected(ctx, out, p.Scope, p.Socket, selected.ID, selected.Name)
				cancel()
			}
			if err != nil {
				fmt.Fprintln(out, "Retry failed:", err)
			}
			waitForReturn(reader, out)
		case "n":
			s.temporaryInstall(reader, out)
		case "g":
			s.guestPlugins(reader, out)
		case "q":
			return nil
		}
	}
}

// Check prints the passive prerequisite snapshot without making repairs.
func (s *Service) Check(out io.Writer) {
	printWorkerStatus(out)
	PrintChecks(out, s.registration.Inspect())
}
