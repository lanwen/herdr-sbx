package panel

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// Repair runs interactive SSH setup and refreshes
// prerequisite status when it finishes.
func (s *Service) Repair(action string, out io.Writer) error {
	if action != "ssh" {
		return fmt.Errorf("unsupported repair action: use ssh")
	}
	title := "Setting up sandbox SSH"
	fmt.Fprintln(out, title+". Follow any prompts in this terminal.")
	err, canceled := s.runSSHSetup()
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

func (s *Service) runSSHSetup() (error, bool) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := s.sbx.SetupSSHInteractive(ctx)
	return err, ctx.Err() != nil
}
