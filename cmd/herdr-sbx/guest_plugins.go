package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/urfave/cli/v3"
)

func guestPluginsCommand(client *herdr.Client) *cli.Command {
	toggle := func(name string, enabled bool) *cli.Command {
		return &cli.Command{Name: name, Usage: "Save whether this plugin is provisioned in guests", ArgsUsage: "<plugin-id>", Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: guest-plugins %s <plugin-id>", name)
			}
			return store.SetGuestPluginDefault(cmd.Args().First(), enabled, nil)
		}}
	}
	return &cli.Command{Name: "guest-plugins", Usage: "Manage opt-in guest plugin defaults", Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Args().Len() != 0 {
			return errors.New("usage: guest-plugins [list|enable|disable|bundle]")
		}
		return guestplugins.PrintChoices(ctx, client, cmd.Root().Writer)
	}, Commands: []*cli.Command{
		{Name: "list", Usage: "Show local plugins and their guest defaults", Action: func(ctx context.Context, cmd *cli.Command) error {
			return guestplugins.PrintChoices(ctx, client, cmd.Root().Writer)
		}},
		toggle("enable", true), toggle("disable", false),
		{Name: "bundle", Usage: "Set a Linux-ready plugin directory without enabling it", ArgsUsage: "<plugin-id> <directory>", Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 2 {
				return errors.New("usage: guest-plugins bundle <plugin-id> <directory>")
			}
			path, err := filepath.Abs(cmd.Args().Get(1))
			if err != nil {
				return err
			}
			return store.SetGuestPluginBundle(cmd.Args().First(), path)
		}},
	}}
}
