// Package registration owns the ordered registration and installation
// transaction for a sandbox. Transport and persistent record formats live in
// the sbx, herdr, and store packages.
package registration

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
)

// Service coordinates sandbox identity, saved ownership, guest preparation,
// and Herdr registration using the typed clients it receives at startup.
type Service struct {
	herdr                 *herdr.Client
	sbx                   *sbx.Client
	inspector             *precheck.Inspector
	runner                process.Runner
	releaseTransport      http.RoundTripper
	preparationRetryDelay time.Duration
	printChecks           func(io.Writer, precheck.Result)
}

// New wires the operation clients used by registration. The same clients can
// be shared with the panel and lifecycle services.
func New(sandboxClient *sbx.Client, herdrClient *herdr.Client, inspector *precheck.Inspector, runner process.Runner) *Service {
	return &Service{
		sbx: sandboxClient, herdr: herdrClient, inspector: inspector, runner: runner,
		releaseTransport: http.DefaultTransport, preparationRetryDelay: 5 * time.Minute,
	}
}

// Inspect returns the passive prerequisite snapshot used before registration.
func (s *Service) Inspect() precheck.Result { return s.inspector.Inspect() }

// SetCheckPrinter supplies the panel-owned report formatter used while
// reconciliation displays its prerequisite gate.
func (s *Service) SetCheckPrinter(printer func(io.Writer, precheck.Result)) { s.printChecks = printer }

func (s *Service) showChecks(out io.Writer, result precheck.Result) {
	if s.printChecks != nil {
		s.printChecks(out, result)
	}
}

func (s *Service) inventory(ctx context.Context, socket string) ([]sbx.Sandbox, error) {
	return s.sbx.Inventory(ctx, socket)
}

// runSSH keeps guest commands bounded and passes each remote shell operation as
// one argument, so local command parsing cannot change its meaning.
func (s *Service) runSSH(ctx context.Context, timeout time.Duration, target, command string) (string, error) {
	return s.runner.CombinedOutput(ctx, process.Command{
		Name: "ssh", Timeout: timeout,
		Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, command},
	})
}
