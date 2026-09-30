package precheck

import (
	"context"
	"errors"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func (i *Inspector) daemonStatus() (sbx.Daemon, bool, Check) {
	daemon, err := i.sandboxes.DaemonStatus(context.Background())
	if err != nil {
		if errors.Is(err, sbx.ErrInvalidDaemonStatus) {
			return sbx.Daemon{}, false, Check{"Sandbox daemon", Unknown, "returned an unrecognised status response", "Check the Docker Sandboxes version and daemon status."}
		}
		return sbx.Daemon{}, false, Check{"Sandbox daemon", Unknown, commandError(err, sbx.CommandOutput(err)), "Run `sbx daemon status --json` after Docker Sandboxes is available."}
	}
	status := strings.ToLower(daemon.Status)
	if status == "running" {
		return daemon, true, Check{"Sandbox daemon", Ready, "running", ""}
	}
	return daemon, false, Check{"Sandbox daemon", Blocked, statusOr(status, "not running"), "Use Docker Sandboxes to start the daemon, then check again."}
}
