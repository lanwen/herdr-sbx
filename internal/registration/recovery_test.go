package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestRegistrationMismatchRecoversThenRegistersOnce(t *testing.T) {
	fixture := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	adds, handoffs, notifications := 0, 0, 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		call := command.Name + " " + strings.Join(command.Args, " ")
		switch call {
		case "herdr --version":
			return "herdr 0.9.3", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/recovery.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "ssh -o BatchMode=yes -o ConnectTimeout=8 demo.sbx " + remoteHerdrStatusShell():
			return remoteHerdrOnPath + "\nherdr 0.9.3", nil
		case "herdr machine list --json":
			if adds < 2 {
				return "[]", nil
			}
			return `[{"id":"machine-1","label":"sbx · demo","target":"demo.sbx","session":"default"}]`, nil
		case "herdr machine add --label sbx · demo demo.sbx":
			adds++
			if handoffs == 0 {
				return "remote herdr server is still running v0.9.4; it will use 0.9.3 after it restarts. error: remote server is not ready for saved machines", errors.New("exit 1")
			}
			return "Saved SSH machine machine-1. Remote server is ready.", nil
		case "herdr --machine machine-1 status server":
			return "running", nil
		}
		if command.Name == "ssh" && strings.Contains(call, "server live-handoff") {
			handoffs++
			return "live handoff complete", nil
		}
		if command.Name == "ssh" && strings.Contains(call, "status server --json") {
			return `{"running":true,"version":"0.9.3"}`, nil
		}
		if command.Name == "herdr" && command.Args[0] == "notification" {
			notifications++
			return "{}", nil
		}
		return "", fmt.Errorf("unexpected call %s", call)
	}
	fixture.rebuildClients()
	for range 3 {
		if err := fixture.service.ReconcileContext(context.Background(), io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	// The intermediate retrying state has no active diagnostic to notify.
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	items := state.Scopes[store.ScopeID("/tmp/recovery.sock")]
	if adds != 2 || handoffs != 1 || notifications != 0 || len(items) != 1 || items[0].Pending || items[0].ProfileID != "machine-1" {
		t.Fatalf("adds=%d handoffs=%d notifications=%d items=%+v", adds, handoffs, notifications, items)
	}
}

func TestRegistrationNotificationsRetryDeliveryAndResetAfterRecovery(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	calls := 0
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name != "herdr" || command.Args[0] != "notification" {
			t.Fatalf("unexpected command: %+v", command)
		}
		calls++
		if calls == 1 {
			return "", errors.New("socket unavailable")
		}
		return "{}", nil
	}
	fixture.rebuildClients()
	issue := result{Name: "demo", Status: "setup required", Detail: "SSH unavailable"}
	if err := saveResultError(store.StatusKey("scope", "id"), issue); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", issue)
	}
	if calls != 2 {
		t.Fatalf("notifications=%d, want failed delivery plus one retry", calls)
	}
	fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", result{Name: "demo", Status: "ready", Detail: "reachable"})
	fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", issue)
	if calls != 3 {
		t.Fatalf("recurrence did not notify: %d", calls)
	}
}

func TestRegistrationNotificationUsesOnePersistedDiagnostic(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	var bodies []string
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name != "herdr" || command.Args[0] != "notification" {
			t.Fatalf("unexpected command: %+v", command)
		}
		for i, arg := range command.Args {
			if arg == "--body" {
				bodies = append(bodies, command.Args[i+1])
			}
		}
		return "{}", nil
	}
	fixture.rebuildClients()
	issue := result{Name: strings.Repeat("界", 40), Status: "manual intervention required", Detail: "combined output", PluginFailures: []store.PluginFailure{
		{PluginID: "z.plugin", Code: store.CodeGuestPluginSync, Detail: "second failure", Output: strings.Repeat("build output\n", 100), Command: "herdr plugin install secret"},
		{PluginID: "a.plugin", Code: store.CodeGuestPluginResponse, Detail: "first failure"},
	}}
	// Without a saved cause there is no reference that the user can find.
	fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", issue)
	if len(bodies) != 0 {
		t.Fatalf("unsaved diagnostic notified: %q", bodies)
	}
	if err := saveResultError(store.StatusKey("scope", "id"), issue); err != nil {
		t.Fatal(err)
	}
	primary, found, err := store.PrimaryError(store.StatusKey("scope", "id"))
	if err != nil || !found || primary.PluginID != "a.plugin" {
		t.Fatalf("primary=%+v found=%v err=%v", primary, found, err)
	}
	for range 3 {
		fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", issue)
	}
	if len(bodies) != 1 {
		t.Fatalf("notifications=%q", bodies)
	}
	body := bodies[0]
	if ansi.StringWidth(body) > 120 || strings.ContainsAny(body, "\r\n") || !regexp.MustCompile(` · [a-z0-9]{6} · `).MatchString(body) || !strings.Contains(body, primary.Ref) || !strings.Contains(body, "Unexpected guest plugin response") || !strings.HasSuffix(body, "more in TUI") || strings.Contains(body, "build output") || strings.Contains(body, "secret") {
		t.Fatalf("notification did not stay concise and findable: %q", body)
	}
	// A confirmed recovery resets delivery deduplication for a later failure.
	ready := result{Name: issue.Name, Status: "ready"}
	if err := saveResultError(store.StatusKey("scope", "id"), ready); err != nil {
		t.Fatal(err)
	}
	fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", ready)
	if err := saveResultError(store.StatusKey("scope", "id"), issue); err != nil {
		t.Fatal(err)
	}
	fixture.service.notifyResult(context.Background(), io.Discard, "scope", "id", issue)
	if len(bodies) != 2 || bodies[0] == bodies[1] {
		t.Fatalf("new failure did not get a fresh notification reference: %q", bodies)
	}
}

func TestRegistrationDiagnosticSaveFailureIsReported(t *testing.T) {
	newRegistrationFixture(t, "[]")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/dev/null/not-a-directory")
	if err := saveResultError("scope", result{Status: "setup required", Detail: "unreachable"}); err == nil {
		t.Fatal("diagnostic persistence failure was discarded")
	}
}
