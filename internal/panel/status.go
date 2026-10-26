package panel

import (
	"fmt"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/precheck"
)

// statusPrerequisites groups related observations for presentation only.
// The inspector retains its independent checks and recovery contracts.
func statusPrerequisites(checks []precheck.Check) []precheck.Check {
	var rows []precheck.Check
	for _, check := range checks {
		var keep bool
		check, keep = groupStatusCheck(rows, check)
		if !keep {
			continue
		}
		check.Detail = statusDetail(check.Detail)
		rows = append(rows, check)
	}
	return rows
}

func mergeStatusChecks(first, next precheck.Check) precheck.Check {
	if next.Level == precheck.Blocked || first.Level == precheck.Ready {
		first.Level = next.Level
	}
	if next.Detail != "" {
		first.Detail += " · " + statusDetail(next.Detail)
	}
	if next.Action != "" && next.Action != first.Action {
		if first.Action != "" {
			first.Action += " "
		}
		first.Action += next.Action
	}
	return first
}

func statusDetail(detail string) string {
	switch detail {
	case "available":
		return "Available"
	case "stopping":
		return "Stopping"
	case "running":
		return "Running"
	case "stopped":
		return "Stopped"
	case "exited":
		return "Exited"
	case "starting":
		return "Starting"
	case "not running":
		return "Not running"
	case "local sandbox inventory is readable":
		return "Inventory readable"
	case "JSON daemon status and inventory are available":
		return "Command support available"
	case "managed sandbox SSH proxy is configured":
		return "Configured"
	}
	return detail
}

// PrintChecks formats the ordered passive prerequisite report.
func PrintChecks(out io.Writer, p precheck.Result) {
	for _, check := range p.Checks {
		fmt.Fprintf(out, "%-10s %-18s %s\n", strings.ToUpper(string(check.Level)), check.Name, check.Detail)
		if check.Action != "" {
			fmt.Fprintf(out, "           Next: %s\n", check.Action)
		}
	}
}

func mergeNamedStatusCheck(rows []precheck.Check, name string, check precheck.Check, readyOnly bool) bool {
	for i := range rows {
		if rows[i].Name != name {
			continue
		}
		if !readyOnly || rows[i].Level == precheck.Ready {
			rows[i] = mergeStatusChecks(rows[i], check)
		}
		return true
	}
	return false
}

func groupStatusCheck(rows []precheck.Check, check precheck.Check) (precheck.Check, bool) {
	switch check.Name {
	case "Sandbox setup":
		return check, false
	case "Sandbox capability":
		if mergeNamedStatusCheck(rows, "sbx", check, true) {
			return check, false
		}
		check.Name = "sbx"
	case "Sandbox daemon":
		check.Name = "Sandbox service"
	case "Sandbox inventory":
		if mergeNamedStatusCheck(rows, "Sandbox service", check, false) {
			return check, false
		}
		check.Name = "Sandbox service"
	case "Docker authorization":
		check.Name = "Docker sign-in"
		if check.Level == precheck.Unknown && strings.HasPrefix(check.Detail, "not checked ") {
			check.Detail, check.Action = "Not checked", ""
		}
	}
	return check, true
}
