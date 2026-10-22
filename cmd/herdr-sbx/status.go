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
	SchemaVersion int                 `json:"schema_version"`
	ReadAt        time.Time           `json:"read_at"`
	Daemon        store.WorkerStatus  `json:"daemon"`
	Management    managementStatus    `json:"management"`
	Errors        []store.ErrorRecord `json:"errors"`
	Warnings      []store.ErrorRecord `json:"warnings"`
}

// statusReport extends the daemon snapshot with sandbox and plugin observations.
type statusReport struct {
	daemonStatusReport
	Sandboxes []statusSandbox           `json:"sandboxes"`
	Plugins   store.GuestPluginDefaults `json:"plugins"`
}

// managementStatus keeps the saved permission separate from daemon health.
type managementStatus struct {
	Enabled     bool   `json:"enabled"`
	Initialized bool   `json:"initialized"`
	Generation  uint64 `json:"generation"`
}

type statusSandbox = store.SandboxStatus

func readStatusReport() (statusReport, error) {
	report := statusReport{Sandboxes: []statusSandbox{}}
	var err error
	report.Daemon, err = store.ReadWorkerStatus()
	if err != nil {
		return report, err
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		return report, err
	}
	report.daemonStatusReport = daemonReport(report.Daemon, saved)
	report.Plugins, err = store.ReadGuestPluginDefaults()
	if err != nil {
		return report, err
	}
	for key, row := range saved.Sandboxes {
		if choice, ok := saved.EnabledChoices[key]; ok {
			row.Enabled = choice
		}
		if row.Enabled == "" {
			row.Enabled = store.Enabled
		}
		if row.Observed == "" {
			row.Observed = "unknown"
		}
		report.Sandboxes = append(report.Sandboxes, row)
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
		fmt.Fprintln(table, "ENABLED\tSANDBOX\tHERDR LABEL\tSTATUS\tRUNTIME\tNATIVE ENABLED\tOBSERVED")
		for _, row := range report.Sandboxes {
			label := row.Label
			if label == "" {
				label = "—"
			} else if row.Stale {
				label += " (last known)"
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
			native := "unknown"
			if row.NativeEnabled != nil {
				native = "OFF"
				if *row.NativeEnabled {
					native = "ON"
				}
			}
			enabled := "Disabled"
			if row.Enabled == store.Enabled {
				enabled = "Enabled"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", enabled, terminalField(row.Name), terminalField(label), terminalField(readiness), terminalField(row.Observed), native, observed)
			if row.Detail != "" {
				fmt.Fprintf(table, "\t%s\n", terminalField(row.Detail))
			}
			if row.AssociationIssue != "" {
				fmt.Fprintf(table, "\t%s\n", terminalField(row.AssociationIssue))
			}
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
	report := daemonStatusReport{SchemaVersion: 4, ReadAt: time.Now(), Daemon: worker, Management: managementStatus{Enabled: true}, Errors: []store.ErrorRecord{}, Warnings: []store.ErrorRecord{}}
	if saved.WorkerControl != nil {
		report.Management = managementStatus{Enabled: !saved.WorkerControl.Stopped, Initialized: saved.WorkerControl.Initialized, Generation: saved.WorkerControl.Generation}
	}
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
			fmt.Fprintf(out, "%s · %s · %s\n", paint(terminalField(item.Code), tone), terminalField(item.Scope), terminalMessage(item.Detail))
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
	switch report.Daemon.State {
	case "running":
		symbol, tone = "✓", "32"
	case "starting", "reconnecting":
		symbol, tone = "…", "33"
	case "cancelling":
		symbol, tone = "…", "2"
	case "unknown":
		symbol, tone = "?", "33"
	}
	fmt.Fprintln(out, paint("herdr-sbx", "1"))
	management, managementTone := "OFF", "2"
	if report.Management.Enabled {
		management, managementTone = "ON", "34"
	}
	fmt.Fprintf(out, "Management: %s\n", paint(management, managementTone))
	state := strings.ToUpper(terminalField(report.Daemon.State))
	if report.Daemon.State == "cancelling" {
		state = "Cancelling"
	}
	fmt.Fprintf(out, "Daemon: %s %s\n", paint(state, tone), paint(symbol, tone))
	if report.Daemon.Detail != "" && report.Daemon.State != "stopped" {
		fmt.Fprintln(out, terminalMessage(report.Daemon.Detail))
	}
	if report.Daemon.LastSync.IsZero() {
		fmt.Fprintln(out, "Last sync: not yet observed")
	} else {
		fmt.Fprintln(out, "Last sync:", report.Daemon.LastSync.Format(time.RFC3339))
	}
}
