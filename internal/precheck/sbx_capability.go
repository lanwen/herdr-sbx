package precheck

import (
	"context"
	"strings"
)

func (i *Inspector) sbxCapability(sbxCheck Check) Check {
	if sbxCheck.Level == Blocked {
		if !strings.HasPrefix(sbxCheck.Detail, "requires ") {
			return Check{"Sandbox capability", Blocked, "Docker Sandboxes is not available", "Install Docker Sandboxes, then check again."}
		}
		return Check{"Sandbox capability", Blocked, "requires sbx " + minSBXVersion + " or later", "Update Docker Sandboxes, then check again."}
	}
	if sbxCheck.Level != Ready {
		return Check{"Sandbox capability", Unknown, "Docker Sandboxes version is unavailable", "Install or update Docker Sandboxes, then check again."}
	}
	if !i.sandboxes.JSONCommandsAvailable(context.Background()) {
		return Check{"Sandbox capability", Blocked, "requires JSON daemon status and sandbox inventory commands", "Update Docker Sandboxes to " + minSBXVersion + " or later, then check again."}
	}
	return Check{"Sandbox capability", Ready, "JSON daemon status and inventory are available", ""}
}
