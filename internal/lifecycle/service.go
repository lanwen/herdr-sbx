// Package lifecycle owns stream sequencing, deletion recovery, and worker
// lifetime. Registration ownership decisions are delegated to registration.
package lifecycle

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Service supervises sandbox event streams and the plugin's detached worker.
type Service struct {
	registrations *registration.Service
	sbx           *sbx.Client
	herdr         *herdr.Client
	printChecks   func(io.Writer, precheck.Result)

	catchupInterval time.Duration
	deleteRetry     time.Duration
	workerPoll      time.Duration
	workerReconnect time.Duration
	serverHandoff   time.Duration
}

// New wires the lifecycle supervisor to shared typed clients.
func New(registrations *registration.Service, sandboxClient *sbx.Client, herdrClient *herdr.Client) *Service {
	return &Service{
		registrations: registrations, sbx: sandboxClient, herdr: herdrClient,
		catchupInterval: 30 * time.Second, deleteRetry: time.Second,
		workerPoll: 5 * time.Second, workerReconnect: time.Second, serverHandoff: 5 * time.Second,
	}
}

// SetCheckPrinter supplies the presentation-layer formatter for watch status.
func (s *Service) SetCheckPrinter(printer func(io.Writer, precheck.Result)) { s.printChecks = printer }

func (s *Service) checks(out io.Writer, result precheck.Result) {
	if s.printChecks != nil {
		s.printChecks(out, result)
	}
}

func hasLevel(result precheck.Result, name string, want precheck.Level) bool {
	for _, check := range result.Checks {
		if check.Name == name && check.Level == want {
			return true
		}
	}
	return false
}

func hasBlocked(result precheck.Result, name string) bool {
	return hasLevel(result, name, precheck.Blocked)
}

func (s *Service) warnBlocking(ctx context.Context, out io.Writer, key, detail string) {
	changed, err := store.WarningTransition(key, detail)
	if err != nil {
		fmt.Fprintln(out, "Sandbox warning state was not saved:", err)
		return
	}
	if !changed {
		return
	}
	fmt.Fprintln(out, "Docker Sandboxes warning:", detail)
	_ = s.herdr.Notify(ctx, "Docker Sandboxes", detail)
}
