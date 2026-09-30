package panel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

func (s *Service) repair(action string, reader *bufio.Reader, out io.Writer) {
	if err := s.Repair(action, out); err != nil {
		fmt.Fprintln(out, "Action failed:", err)
	}
	waitForReturn(reader, out)
}

// Repair runs exactly one supported interactive setup action and refreshes
// prerequisite status when it finishes.
func (s *Service) Repair(action string, out io.Writer) error {
	titles := map[string]string{
		"daemon": "Starting the sandbox daemon",
		"login":  "Opening Docker sign-in",
		"ssh":    "Setting up sandbox SSH",
	}
	title, ok := titles[action]
	if !ok {
		return fmt.Errorf("unknown repair action")
	}
	fmt.Fprintln(out, title+". Follow any prompts in this terminal.")
	err, canceled := s.runRepair(action)
	switch {
	case canceled:
		fmt.Fprintln(out, title+" canceled.")
	case err != nil:
		fmt.Fprintln(out, title+" failed:", err)
	default:
		fmt.Fprintln(out, title+" finished.")
	}
	fmt.Fprintln(out, "Refreshing status.")
	PrintChecks(out, s.registration.Inspect())
	return nil
}

func (s *Service) runRepair(action string) (error, bool) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := s.sbx.RepairInteractive(ctx, action)
	return err, ctx.Err() != nil
}

func waitForReturn(reader *bufio.Reader, out io.Writer) {
	fmt.Fprintln(out, "Press Enter to return to status.")
	_, _ = reader.ReadString('\n')
}
