package precheck

import (
	"context"
	"strings"
)

func (i *Inspector) sandboxInventory(socket string) ([]Check, bool) {
	sandboxes, err := i.inventory(context.Background(), socket)
	if err != nil {
		return ClassifyInventoryError(err, ""), false
	}
	if len(sandboxes) == 0 {
		return []Check{{"Sandbox inventory", Unknown, "empty inventory may be incomplete", "Check Docker Sandboxes, then check again."}}, true
	}
	return []Check{{"Sandbox inventory", Ready, "local sandbox inventory is readable", ""}}, true
}

// ClassifyInventoryError turns a daemon inventory failure into visible checks.
func ClassifyInventoryError(err error, output string) []Check {
	detail := commandError(err, output)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "docker login service unavailable") || strings.Contains(lower, "not logged in") || strings.Contains(lower, "authentication required") || strings.Contains(lower, "unauthorized") {
		return []Check{{"Docker authorization", Blocked, detail, "Sign in with `sbx login`, then check again."}}
	}
	if strings.Contains(lower, "daemon") || strings.Contains(lower, "socket") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "backend") {
		return []Check{
			{"Sandbox inventory", Unknown, detail, "Check the Docker Sandboxes daemon, then check again."},
			{"Docker authorization", Unknown, "not checked because the sandbox backend is unavailable", "Restore daemon access, then check again."},
		}
	}
	return []Check{
		{"Sandbox inventory", Unknown, detail, "Check the Docker Sandboxes backend, then check again."},
		{"Docker authorization", Unknown, "not checked because sandbox inventory failed", "Check Docker authorization after inventory recovers."},
	}
}
