package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/lifecycle"
	"github.com/lanwen/herdr-sbx/internal/panel"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/urfave/cli/v3"
)

const rootUsage = "usage: herdr-sbx [panel|status|guest-plugins|adopt|check|open-status|open-status-tab|open-status-split|repair|reconcile|watch|worker|daemon|retry-preparation]"

func commands() *cli.Command {
	runner := process.Runner{}
	sandboxClient := sbx.New(runner)
	herdrClient := herdr.New(runner)
	inspector := precheck.New(sandboxClient, herdrClient, runner)
	registrations := registration.New(sandboxClient, herdrClient, inspector, runner)
	lifecycleService := lifecycle.New(registrations, sandboxClient, herdrClient)
	ui := panel.New(registrations, lifecycleService, sandboxClient)
	command := func(name, usage string, action cli.ActionFunc) *cli.Command {
		return &cli.Command{Name: name, Usage: usage, Action: action}
	}
	return &cli.Command{
		Name:      "herdr-sbx",
		Usage:     "Manage Herdr registrations for Docker Sandboxes",
		Writer:    os.Stdout,
		ErrWriter: os.Stderr,
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return errors.New(rootUsage)
			}
			return ui.Run(os.Stdin, os.Stdout)
		},
		Commands: []*cli.Command{
			guestPluginsCommand(herdrClient),
			{Name: "adopt", Usage: "Adopt an existing matching sandbox machine without reconnecting it", ArgsUsage: "<sandbox-name> <machine-id>", Action: func(ctx context.Context, cmd *cli.Command) error {
				if cmd.Args().Len() != 2 {
					return errors.New("usage: herdr-sbx adopt <sandbox-name> <machine-id>")
				}
				if err := registrations.AdoptMachine(ctx, cmd.Args().First(), cmd.Args().Get(1)); err != nil {
					return err
				}
				fmt.Fprintln(cmd.Root().Writer, "Adopted existing machine. The plugin now manages its lifecycle.")
				return nil
			}},
			command("status", "Show background worker status and last successful sync", func(_ context.Context, cmd *cli.Command) error {
				if cmd.Args().Len() != 0 {
					return errors.New("usage: herdr-sbx status")
				}
				return panel.PrintWorkerStatus(cmd.Root().Writer)
			}),
			command("panel", "Show status and interactive actions", func(_ context.Context, _ *cli.Command) error {
				return ui.Run(os.Stdin, os.Stdout)
			}),
			command("check", "Inspect prerequisites without changing setup", func(_ context.Context, _ *cli.Command) error {
				ui.Check(os.Stdout)
				return nil
			}),
			command("open-status", "Open the status panel", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "popup") }),
			command("open-status-tab", "Open status in a tab", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "tab") }),
			command("open-status-split", "Open status in a split", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "split") }),
			command("reconcile", "Register prepared running sandboxes", func(_ context.Context, _ *cli.Command) error { return registrations.Reconcile(os.Stdout) }),
			repairCommand(ui),
			command("watch", "Watch sandbox lifecycle events", func(_ context.Context, _ *cli.Command) error { return lifecycleService.Watch(os.Stdout) }),
			command("open-watch", "Open the lifecycle watcher in a tab", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "watch", "tab") }),
			command("worker", "Run the plugin worker", func(_ context.Context, _ *cli.Command) error { return lifecycleService.Worker(os.Stdout) }),
			command("open-worker", "Open the worker in a tab", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "worker", "tab") }),
			command("daemon", "Start a detached plugin worker", func(_ context.Context, _ *cli.Command) error { return lifecycleService.StartWorker() }),
			retryPreparationCommand(registrations),
		},
	}
}

func repairCommand(ui *panel.Service) *cli.Command {
	return &cli.Command{
		Name:      "repair",
		Usage:     "Set up sandbox SSH interactively",
		ArgsUsage: "ssh",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 || cmd.Args().First() != "ssh" {
				return fmt.Errorf("usage: herdr-sbx repair ssh")
			}
			return ui.Repair(cmd.Args().First(), os.Stdout)
		},
	}
}

func retryPreparationCommand(registrations *registration.Service) *cli.Command {
	return &cli.Command{
		Name:      "retry-preparation",
		Usage:     "Retry Herdr preparation for one sandbox",
		ArgsUsage: "<sandbox-name>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: herdr-sbx retry-preparation <sandbox-name>")
			}
			return registrations.RetryPreparation(ctx, os.Stdout, cmd.Args().First())
		},
	}
}
