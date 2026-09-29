package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
)

var interactiveRun = runInteractive
var errInteractiveCanceled = errors.New("interactive action canceled")

func repair(kind string, out io.Writer) error {
	var args []string
	var title string
	switch kind {
	case "daemon":
		args, title = []string{"daemon", "start"}, "Starting the sandbox daemon"
	case "login":
		args, title = []string{"login"}, "Opening Docker sign-in"
	case "ssh":
		args, title = []string{"setup", "ssh"}, "Setting up sandbox SSH"
	default:
		return errors.New("unknown repair action")
	}

	fmt.Fprintln(out, title+". Follow any prompts in this terminal.")
	if err := interactiveRun("sbx", sbxArgs(args...)...); err != nil {
		if errors.Is(err, errInteractiveCanceled) {
			fmt.Fprintln(out, title+" canceled.")
		} else {
			fmt.Fprintln(out, title+" failed:", err)
		}
	} else {
		fmt.Fprintln(out, title+" finished.")
	}
	fmt.Fprintln(out, "Refreshing status.")
	printChecks(out, inspect())
	return nil
}

func runInteractive(name string, args ...string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	select {
	case err := <-done:
		return err
	case <-interrupt:
		cancel()
		<-done
		return errInteractiveCanceled
	}
}
