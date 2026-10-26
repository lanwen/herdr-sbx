package main

import (
	"context"
	"errors"
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

const rootUsage = "usage: herdr-sbx [status [--json] | daemon <start|stop|status>]"

func commands() *cli.Command {
	runner := process.Runner{}
	sandboxClient := sbx.New(runner)
	herdrClient := herdr.New(runner)
	inspector := precheck.New(sandboxClient, herdrClient, runner)
	registrations := registration.New(sandboxClient, herdrClient, inspector, runner)
	lifecycleService := lifecycle.New(registrations, sandboxClient, herdrClient)
	ui := panel.New(registrations, lifecycleService, sandboxClient)
	command := func(name, usage string, action cli.ActionFunc) *cli.Command {
		return &cli.Command{Name: name, Usage: usage, Hidden: true, Action: action}
	}
	return &cli.Command{
		Name:      "herdr-sbx",
		Usage:     "Manage herdr registrations for Docker Sandboxes",
		Writer:    os.Stdout,
		ErrWriter: os.Stderr,
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return errors.New(rootUsage)
			}
			return ui.Run(os.Stdin, os.Stdout)
		},
		Commands: []*cli.Command{
			{
				Name: "status", Usage: "Show cached sandbox and daemon status",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "Print a detailed JSON snapshot"}},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return errors.New("usage: herdr-sbx status [--json]")
					}
					return printStatus(cmd.Root().Writer, cmd.Bool("json"))
				},
			},
			command("open-status", "Open the sandbox manager", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "popup") }),
			command("open-status-tab", "Open the sandbox manager in a tab", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "tab") }),
			command("open-status-split", "Open the sandbox manager in a split", func(ctx context.Context, _ *cli.Command) error { return herdrClient.OpenPane(ctx, "status", "split") }),
			command("worker", "Internal worker process", func(_ context.Context, _ *cli.Command) error {
				return lifecycleService.Worker(terminalWriter{out: os.Stdout})
			}),
			daemonCommand(lifecycleService),
		},
	}
}

func daemonCommand(lifecycleService *lifecycle.Service) *cli.Command {
	return &cli.Command{
		Name: "daemon", Usage: "Manage the background daemon",
		Action: func(_ context.Context, _ *cli.Command) error {
			return errors.New("usage: herdr-sbx daemon <start|stop|status>")
		},
		Commands: []*cli.Command{
			{
				Name: "start", Usage: "Turn Management ON and start the daemon",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "automatic", Hidden: true}},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return errors.New("usage: herdr-sbx daemon start")
					}
					if cmd.Bool("automatic") {
						return lifecycleService.StartWorker()
					}
					return lifecycleService.StartWorkerExplicit()
				},
			},
			{
				Name: "stop", Usage: "Turn Management OFF, cancel automatic work, and stop the daemon",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return errors.New("usage: herdr-sbx daemon stop")
					}
					return lifecycleService.StopWorker(ctx)
				},
			},
			{
				Name: "status", Usage: "Show cached daemon status",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "Print a detailed daemon JSON snapshot"}},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return errors.New("usage: herdr-sbx daemon status [--json]")
					}
					return printDaemonStatus(cmd.Root().Writer, cmd.Bool("json"))
				},
			},
		},
	}
}
