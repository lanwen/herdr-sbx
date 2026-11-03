package registration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestRegistrationIdentityFailureDoesNotMisreportInventoryFailure(t *testing.T) {
	cases := []struct {
		name, inventory, want string
		readErr               error
	}{
		{"inventory unavailable", "", "inventory temporarily unavailable", errors.New("inventory temporarily unavailable")},
		{"missing identity", `[]`, "not present", nil},
		{"replaced identity", `[{"uuid":"replacement","name":"demo","status":"running"}]`, "different identity", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRegistrationFixture(t, `[]`)
			fixture.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				if test.readErr != nil {
					return nil, test.readErr
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.inventory))}, nil
			})
			fixture.rebuildClients()
			expected := sbx.Sandbox{ID: "expected", Name: "demo", Status: "running"}
			state := store.State{Version: 1, Scopes: map[string][]store.Tracked{}}
			scan := reconciliation{service: fixture.service, ctx: context.Background(), prerequisites: precheck.Result{Scope: "scope", Socket: "/tmp/identity-test.sock"}}
			current := scan.registrationCurrent(expected)
			outcome, _ := fixture.service.registerPreparedContext(context.Background(), registrationWork{Scope: "scope", State: &state, Sandbox: expected, Current: current, CurrentError: func() error { return scan.currentError }}, nil)
			require.Contains(t, outcome.Detail, test.want)
		})
	}
}
