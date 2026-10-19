package registration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestFixGuestHerdrValidatesOwnershipAndIdentityBeforeSSH(t *testing.T) {
	for _, scenario := range []string{"scope changed", "replacement", "stopped", "unwatched", "pending", "target changed", "catalog changed", "host changed"} {
		t.Run(scenario, func(t *testing.T) {
			f, scope, sshCalls, _ := newFixHerdrFixture(t, false)
			socket := "/tmp/fix-herdr.sock"
			state, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "scope changed":
				socket = "/tmp/other.sock"
			case "replacement":
				f.inventory = `[{"uuid":"replacement","name":"demo","status":"running"}]`
			case "stopped":
				f.inventory = `[{"uuid":"sandbox-1","name":"demo","status":"stopped"}]`
			case "unwatched":
				if err := store.SetManagement(scope, "sandbox-1", store.Unwatched); err != nil {
					t.Fatal(err)
				}
			case "pending":
				state.Scopes[scope][0].Pending = true
			case "target changed":
				state.Scopes[scope][0].Target = "other.sbx"
			case "catalog changed":
				state.Scopes[scope][0].ProfileID = "other"
			}
			if err := store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			expected := "0.9.3"
			if scenario == "host changed" {
				expected = "0.9.2"
			}
			if err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, socket, "sandbox-1", "demo", expected); err == nil {
				t.Fatal("unsafe fix accepted")
			}
			if *sshCalls != 0 {
				t.Fatalf("SSH before guard: %d", *sshCalls)
			}
		})
	}
}

func TestFixGuestHerdrHandoffAndReconcile(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprint("handoff failure=", fails), func(t *testing.T) {
			f, scope, _, handoffs := newFixHerdrFixture(t, fails)
			key := store.StatusKey(scope, "sandbox-1")
			if err := store.SetRegistrationError(store.CodeHerdrCompatibility, key, "existing incompatibility"); err != nil {
				t.Fatal(err)
			}
			err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, "/tmp/fix-herdr.sock", "sandbox-1", "demo", "herdr 0.9.3")
			if (err != nil) != fails {
				t.Fatalf("error=%v", err)
			}
			if *handoffs != 1 {
				t.Fatalf("handoffs=%d", *handoffs)
			}
			status, readErr := store.ReadStatusStore()
			if readErr != nil {
				t.Fatal(readErr)
			}
			seen := status.Sandboxes[key].GuestServer
			if fails {
				if len(status.Errors) != 1 || seen != nil && seen.Current {
					t.Fatalf("failure claimed recovery: %+v", status)
				}
			} else if len(status.Errors) != 0 || seen == nil || seen.Version != "0.9.3" || !seen.Current || status.Sandboxes[key].Connection != "ready" {
				t.Fatalf("recovery was not observed: %+v", status)
			}
			state, err := store.LoadState()
			if err != nil || state.Scopes[scope][0].ProfileID != "machine-1" || !state.Scopes[scope][0].ProjectWorkspaceDone {
				t.Fatalf("ownership/workspace altered: %+v %v", state, err)
			}
		})
	}
}

func TestFixGuestHerdrRejectsReplacementBeforeHandoff(t *testing.T) {
	for _, response := range []string{`{"running":true,"version":"0.9.2"}`, `{"running":true,"version":"0.9.3"}`, `{"running":false}`} {
		t.Run(response, func(t *testing.T) {
			f, scope, _, handoffs := newFixHerdrFixture(t, false)
			execute := f.runner.Execute
			f.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
				output, err := execute(ctx, command)
				if command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "status server --json") {
					f.inventory = `[{"uuid":"replacement","name":"demo","status":"running"}]`
					output = response
				}
				return output, err
			}
			f.rebuildClients()
			err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, "/tmp/fix-herdr.sock", "sandbox-1", "demo", "0.9.3")
			if !errors.Is(err, context.Canceled) || *handoffs != 0 {
				t.Fatalf("replacement received handoff: handoffs=%d error=%v", *handoffs, err)
			}
		})
	}
}

func TestFixGuestHerdrRejectsReplacementDuringHandoffVerification(t *testing.T) {
	f, scope, _, handoffs := newFixHerdrFixture(t, false)
	execute := f.runner.Execute
	f.runner.Execute = func(ctx context.Context, command process.Command) (string, error) {
		output, err := execute(ctx, command)
		if command.Name == "ssh" && strings.Contains(strings.Join(command.Args, " "), "status server --json") && *handoffs > 0 {
			f.inventory = `[{"uuid":"replacement","name":"demo","status":"running"}]`
		}
		return output, err
	}
	f.rebuildClients()
	err := f.service.FixGuestHerdr(context.Background(), io.Discard, scope, "/tmp/fix-herdr.sock", "sandbox-1", "demo", "0.9.3")
	if !errors.Is(err, context.Canceled) || *handoffs != 1 {
		t.Fatalf("replacement reported recovery: handoffs=%d error=%v", *handoffs, err)
	}
}

func newFixHerdrFixture(t *testing.T, failHandoff bool) (*registrationFixture, string, *int, *int) {
	t.Helper()
	f := newRegistrationFixture(t, `[{"uuid":"sandbox-1","name":"demo","status":"running"}]`)
	scope := store.ScopeID("/tmp/fix-herdr.sock")
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{scope: {{SandboxID: "sandbox-1", Label: "custom label", Target: "demo.sbx", ProfileID: "machine-1", ProjectWorkspaceDone: true}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetManagement(scope, "sandbox-1", store.Watched); err != nil {
		t.Fatal(err)
	}
	if err := store.ObserveSandbox(scope, sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	sshCalls, handoffs := 0, 0
	f.runner.Execute = func(_ context.Context, c process.Command) (string, error) {
		args := strings.Join(c.Args, " ")
		switch c.Name + " " + args {
		case "herdr --version":
			return "herdr 0.9.3", nil
		case "sbx version":
			return "sbx 0.45.1", nil
		case "ssh -V":
			return "OpenSSH", nil
		case "sbx daemon status --help", "sbx ls --help":
			return "--json", nil
		case "sbx daemon status --json":
			return `{"status":"running","socket":"/tmp/fix-herdr.sock"}`, nil
		case "sbx diagnose --json":
			return `{"checks":[{"name":"Authentication","status":"pass"}]}`, nil
		case "ssh -G herdr-sandbox-probe.sbx":
			return "proxycommand sbx ssh proxy %n", nil
		case "herdr machine list --json":
			return `[{"id":"machine-1","label":"custom label","target":"demo.sbx","session":"default"}]`, nil
		case "herdr plugin list --json":
			return `{"result":{"plugins":[]}}`, nil
		}
		if c.Name == "ssh" {
			sshCalls++
			if strings.HasSuffix(args, remoteHerdrStatusShell()) {
				return remoteHerdrOnPath + "\nherdr 0.9.3", nil
			}
			if strings.Contains(args, "server live-handoff") {
				handoffs++
				if failHandoff {
					return "unsupported live handoff", errors.New("exit 1")
				}
				return "complete", nil
			}
			if strings.Contains(args, "status server --json") {
				if handoffs == 0 {
					return `{"running":true,"version":"0.9.2"}`, nil
				}
				return `{"running":true,"version":"0.9.3"}`, nil
			}
		}
		return "", fmt.Errorf("unexpected command %s %s", c.Name, args)
	}
	f.rebuildClients()
	return f, scope, &sshCalls, &handoffs
}
