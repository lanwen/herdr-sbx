package panel

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// PrintChecks formats the ordered passive prerequisite report.
func PrintChecks(out io.Writer, p precheck.Result) {
	for _, check := range p.Checks {
		fmt.Fprintf(out, "%-10s %-18s %s\n", strings.ToUpper(string(check.Level)), check.Name, check.Detail)
		if check.Action != "" {
			fmt.Fprintf(out, "           Next: %s\n", check.Action)
		}
	}
}

func printSandboxStatuses(out io.Writer, scope string) {
	snapshot, err := store.Snapshot(scope)
	if err != nil {
		fmt.Fprintln(out, "Sandbox status is unavailable:", err)
		return
	}
	for _, attempt := range snapshot.Installations {
		fmt.Fprintf(out, "WARNING: %s has pending temporary network access cleanup. %s\n", attempt.Sandbox.Name, attempt.Detail)
	}
	if len(snapshot.Sandboxes) == 0 {
		return
	}
	items := snapshot.Sandboxes
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Sandbox readiness")
	for _, status := range items {
		connection := status.Connection
		if status.Stale || (!status.ObservedAt.IsZero() && time.Since(status.ObservedAt) > time.Minute) || (!status.Updated.IsZero() && time.Since(status.Updated) > 2*time.Minute) {
			connection += " (stale)"
		}
		fmt.Fprintf(out, "%-24s observed: %-10s connection: %-18s %s\n", status.Name, status.Observed, connection, status.Detail)
	}
}

func printWorkerStatus(out io.Writer) {
	if err := PrintWorkerStatus(out); err != nil {
		fmt.Fprintln(out, "Worker: UNKNOWN —", err)
	}
	fmt.Fprintln(out, "Press b to start the worker; c to refresh status.")
	fmt.Fprintln(out)
}

// PrintWorkerStatus reports only worker health; it never probes or repairs
// sandbox prerequisites. Read failures are returned to command callers.
func PrintWorkerStatus(out io.Writer) error {
	status, err := store.ReadWorkerStatus()
	if err != nil {
		return err
	}
	sync := "not yet"
	if !status.LastSync.IsZero() {
		sync = status.LastSync.Local().Format("2006-01-02 15:04:05 MST")
	}
	fmt.Fprintf(out, "Worker: %s | Last successful sync: %s\n", strings.ToUpper(status.State), sync)
	fmt.Fprintln(out, status.Detail)
	return nil
}
