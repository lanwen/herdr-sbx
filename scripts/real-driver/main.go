// This test-only driver preserves the opt-in real fixture gate after removal
// of the public reconcile command. scripts/test-real.sh validates the fixture.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func run() error {
	if os.Getenv("HERDR_SANDBOX_TEST_ROOT") == "" || os.Getenv("HERDR_SANDBOX_TEST_NAME") == "" {
		return errors.New("run through scripts/test-real.sh with its dedicated fixture")
	}
	runner := process.Runner{}
	sandboxClient := sbx.New(runner)
	herdrClient := herdr.New(runner)
	service := registration.New(sandboxClient, herdrClient, precheck.New(sandboxClient, herdrClient, runner), runner)
	if len(os.Args) == 1 {
		return service.Reconcile(os.Stdout)
	}
	if len(os.Args) != 2 || os.Args[1] != "temporary" || os.Getenv("HERDR_SANDBOX_TEST_TEMPORARY_ACCESS") != "1" {
		return errors.New("temporary access requires the explicit fixture opt-in")
	}
	checks := service.Inspect()
	items, err := service.SandboxInventory(context.Background(), checks.Socket)
	if err != nil {
		return err
	}
	if len(items) != 1 || items[0].Name != os.Getenv("HERDR_SANDBOX_TEST_NAME") {
		return errors.New("dedicated sandbox identity does not match")
	}
	return service.ReconcileAuthorized(context.Background(), os.Stdout, checks.Scope, checks.Socket, items[0].ID, items[0].Name)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
