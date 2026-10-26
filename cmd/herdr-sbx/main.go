package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Docker Sandboxes:", terminalMessage(err.Error()))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := commands().Run(ctx, os.Args)
	if err != nil && strings.HasPrefix(err.Error(), "No help topic for '") {
		err = errors.New(rootUsage)
	}
	return err
}
