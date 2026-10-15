package panel

import (
	"fmt"
	"io"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/precheck"
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
