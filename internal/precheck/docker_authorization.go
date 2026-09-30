package precheck

import (
	"context"

	"github.com/lanwen/herdr-sbx/internal/sbx"
)

func (i *Inspector) dockerAuthorization() Check {
	diagnostic := i.sandboxes.DiagnoseAuthentication(context.Background())
	if !diagnostic.Available {
		return Check{"Docker authorization", Unknown, "Docker sign-in diagnostics are unavailable", "Run `sbx diagnose --json` to check diagnostic support and availability."}
	}
	switch diagnostic.Status {
	case sbx.AuthenticationPass:
		return Check{"Docker authorization", Ready, "Docker sign-in verified", ""}
	case sbx.AuthenticationFail:
		return Check{"Docker authorization", Blocked, "Docker sign-in check failed", "Sign in with `sbx login`, then check again."}
	default:
		return Check{"Docker authorization", Unknown, "Docker sign-in could not be verified", "Run `sbx diagnose --json` to inspect the Authentication result."}
	}
}
