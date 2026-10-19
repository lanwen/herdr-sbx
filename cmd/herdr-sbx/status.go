package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// statusReport is a versioned, read-only view of saved observations.
type daemonStatusReport struct {
	SchemaVersion int                  `json:"schema_version"`
	ReadAt        time.Time            `json:"read_at"`
	Worker        store.WorkerStatus   `json:"worker"`
	WorkerControl *store.WorkerControl `json:"worker_control"`
	Errors        []store.ErrorRecord  `json:"errors"`
	Warnings      []store.ErrorRecord  `json:"warnings"`
}

// statusReport extends the daemon snapshot with sandbox and plugin observations.
type statusReport struct {
	daemonStatusReport
	Sandboxes []statusSandbox           `json:"sandboxes"`
	Plugins   store.GuestPluginDefaults `json:"plugins"`
}

// statusSandbox separates requested management from a removal still in progress.
type statusSandbox struct {
	store.SandboxStatus
	DesiredManagement store.ManagementChoice `json:"desired_management"`
}

func readStatusReport() (statusReport, error) {
	report := statusReport{Sandboxes: []statusSandbox{}}
	var err error
	report.Worker, err = store.ReadWorkerStatus()
	if err != nil {
		return report, err
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		return report, err
	}
	report.daemonStatusReport = daemonReport(report.Worker, saved)
	report.Plugins, err = store.ReadGuestPluginDefaults()
	if err != nil {
		return report, err
	}
	owned, err := store.LoadState()
	if err != nil {
		return report, err
	}
	for key, row := range saved.Sandboxes {
		outstanding := false
		if choice, ok := saved.Management[key]; ok {
			row.Management = choice
		}
		for _, registration := range owned.Scopes[row.Scope] {
			if registration.SandboxID == row.SandboxID {
				outstanding = registration.Pending || registration.ProfileID != ""
				// Ownership journals also contain unresolved intent. Never promote it
				// to verified ownership or replace an observed unmanaged profile.
				if row.Label == "" && row.ProfileID == "" && !registration.Pending && row.Management != store.Unmanaged {
					row.Label, row.ProfileID = registration.Label, registration.ProfileID
				}
				if row.Management == "" {
					row.Management = store.Watched
				}
				break
			}
		}
		if row.Management == "" {
			row.Management = store.Untracked
		}
		if row.Observed == "" {
			row.Observed = "unknown"
		}
		desired := row.Management
		if desired == store.Disabled && (row.Owned || outstanding) {
			row.Management = store.Unwatched
		}
		report.Sandboxes = append(report.Sandboxes, statusSandbox{SandboxStatus: row, DesiredManagement: desired})
	}
	sort.Slice(report.Sandboxes, func(i, j int) bool {
		a, b := report.Sandboxes[i], report.Sandboxes[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return store.StatusKey(a.Scope, a.SandboxID) < store.StatusKey(b.Scope, b.SandboxID)
	})
	for scope, attempt := range saved.Installations {
		report.Errors = append(report.Errors, store.ErrorRecord{Code: store.CodePolicyCleanup, Scope: scope, Detail: attempt.Detail})
	}
	for _, item := range saved.Errors {
		if !strings.HasPrefix(item.Code, "D") {
			report.addIssue(item)
		}
	}
	report.sortIssues()
	return report, nil
}

func printStatus(out io.Writer, asJSON bool) error {
	report, err := readStatusReport()
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	paint := statusPainter(out)
	printDaemonSummary(out, report.daemonStatusReport, paint)
	fmt.Fprintln(out, "\nCached sandbox observations")
	if len(report.Sandboxes) == 0 {
		fmt.Fprintln(out, "No sandbox observations yet. Open the manager to discover sandboxes.")
	} else {
		table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "MANAGEMENT\tSANDBOX\tRUNTIME\tHERDR LABEL\tREADINESS\tOBSERVED")
		for _, row := range report.Sandboxes {
			label := row.Label
			if label == "" {
				label = "—"
			}
			readiness := row.Connection
			if readiness == "" {
				readiness = "unknown"
			}
			observed := "unknown"
			if !row.ObservedAt.IsZero() {
				observed = row.ObservedAt.Format(time.RFC3339)
			}
			if row.Stale {
				observed += " (stale)"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Management, row.Name, row.Observed, label, readiness, observed)
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintln(out)
	printStatusIssues(out, report.daemonStatusReport, paint)
	return nil
}

// daemonReport uses the same fields and schema as the daemon subset of root status.
func daemonReport(worker store.WorkerStatus, saved store.StatusStore) daemonStatusReport {
	report := daemonStatusReport{SchemaVersion: 3, ReadAt: time.Now(), Worker: worker, WorkerControl: saved.WorkerControl, Errors: []store.ErrorRecord{}, Warnings: []store.ErrorRecord{}}
	if worker.State == "reconnecting" {
		report.Errors = append(report.Errors, store.ErrorRecord{Code: store.CodeDaemonReconnect, Scope: "daemon", Detail: worker.Detail})
	}
	for _, item := range saved.Errors {
		if strings.HasPrefix(item.Code, "D") {
			report.addIssue(item)
		}
	}
	report.sortIssues()
	return report
}

func (report *daemonStatusReport) addIssue(item store.ErrorRecord) {
	if store.SeverityForCode(item.Code) == store.SeverityWarning {
		report.Warnings = append(report.Warnings, item)
	} else {
		report.Errors = append(report.Errors, item)
	}
}

func (report *daemonStatusReport) sortIssues() {
	for _, items := range [][]store.ErrorRecord{report.Errors, report.Warnings} {
		sort.Slice(items, func(i, j int) bool {
			return items[i].Scope+items[i].Code < items[j].Scope+items[j].Code
		})
	}
}

func printStatusIssues(out io.Writer, report daemonStatusReport, paint func(string, string) string) {
	fmt.Fprintf(out, "Errors: %d · Warnings: %d\n", len(report.Errors), len(report.Warnings))
	for _, items := range [][]store.ErrorRecord{report.Errors, report.Warnings} {
		for _, item := range items {
			tone := "31"
			if store.SeverityForCode(item.Code) == store.SeverityWarning {
				tone = "33"
			}
			fmt.Fprintf(out, "%s · %s · %s\n", paint(item.Code, tone), item.Scope, item.Detail)
		}
	}
}

func printDaemonStatus(out io.Writer, asJSON bool) error {
	worker, err := store.ReadWorkerStatus()
	if err != nil {
		return err
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	report := daemonReport(worker, saved)
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	paint := statusPainter(out)
	printDaemonSummary(out, report, paint)
	printStatusIssues(out, report, paint)
	return nil
}

func statusPainter(out io.Writer) func(string, string) string {
	color := false
	if file, ok := out.(*os.File); ok {
		color = term.IsTerminal(file.Fd())
	}
	if _, off := os.LookupEnv("NO_COLOR"); off {
		color = false
	}
	if os.Getenv("TERM") == "dumb" {
		color = false
	}
	return func(value, code string) string {
		if color {
			return "\x1b[" + code + "m" + value + "\x1b[0m"
		}
		return value
	}
}

func printDaemonSummary(out io.Writer, report daemonStatusReport, paint func(string, string) string) {
	symbol, tone := "—", "2"
	switch report.Worker.State {
	case "running":
		symbol, tone = "✓", "32"
	case "starting", "reconnecting":
		symbol, tone = "…", "33"
	case "unknown":
		symbol, tone = "?", "33"
	}
	fmt.Fprintln(out, paint("herdr-sbx", "1"))
	fmt.Fprintf(out, "Daemon: %s %s\n", strings.ToUpper(report.Worker.State), paint(symbol, tone))
	if report.Worker.Detail != "" {
		fmt.Fprintln(out, report.Worker.Detail)
	}
	if report.Worker.LastSync.IsZero() {
		fmt.Fprintln(out, "Last sync: not yet observed")
	} else {
		fmt.Fprintln(out, "Last sync:", report.Worker.LastSync.Format(time.RFC3339))
	}
}
