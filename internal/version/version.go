// Package version shares the numeric version checks used by prerequisite
// inspection and guest installation.
package version

import (
	"fmt"
	"regexp"
	"strings"
)

var semver = regexp.MustCompile(`(?m)(?:^|\s|v)(\d+)\.(\d+)\.(\d+)(?:\s|$|[-+])`)

// Parse extracts the numeric major.minor.patch version from CLI output.
// Prerelease and build suffixes are omitted, matching the compatibility gates.
func Parse(value string) string {
	match := semver.FindStringSubmatch(value)
	if len(match) == 4 {
		return match[1] + "." + match[2] + "." + match[3]
	}
	return ""
}

// Compare orders numeric versions returned by Parse. It is not a general
// semantic-version comparator for prerelease identifiers.
func Compare(left, right string) int {
	parse := func(value string) []int {
		parts := strings.Split(value, ".")
		result := make([]int, 3)
		for i := range result {
			if i < len(parts) {
				fmt.Sscanf(parts[i], "%d", &result[i])
			}
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
