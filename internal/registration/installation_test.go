package registration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestTemporaryInstallationOwnershipAndRecovery(t *testing.T) {
	for _, scenario := range []string{
		"success", "cleanup failure", "ambiguous acknowledgement", "cancel", "recreated",
		"non-network failure", "modified rule", "partial error", "rejected allowance", "blocked", "concurrent user rule", "existing access",
	} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRegistrationFixture(t, "[]")
			policy := &policyFixture{scenario: scenario, rules: []sbx.PolicyRule{{
				ID: "user-rule", SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http",
				Targets: []sbx.PolicyAccess{{Host: "updates.example:443", Path: "/manual"}}, Methods: []string{"GET"}, Editable: true, Status: "active",
			}}}
			if scenario == "existing access" {
				policy.rules = append(policy.rules, sbx.PolicyRule{ID: "existing-installer", SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{installerAccess[0]}, Methods: []string{"GET"}, Editable: true, Status: "active"})
			}
			policy.failRemovals = scenario == "cleanup failure"
			fixture.transport = policy
			installed := false
			statusChecks := 0
			current := true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
				if command.Name != "ssh" || len(command.Args) == 0 {
					return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
				}
				switch command.Args[len(command.Args)-1] {
				case remoteHerdrStatusShell():
					statusChecks++
					if !installed {
						return missingRemoteHerdr + "\n", nil
					}
					return remoteHerdrOnPath + "\nherdr 0.9.3\n", nil
				case remoteInstallCommand():
					switch scenario {
					case "cancel":
						cancel()
						return "", context.Canceled
					case "recreated":
						current = false
						return "", context.Canceled
					case "non-network failure":
						return "", fmt.Errorf("no space left on device")
					case "modified rule":
						policy.rules[1].Targets[0].Path = "/user-change/**"
					case "concurrent user rule":
						policy.rules = append(policy.rules, sbx.PolicyRule{ID: "concurrent-user", SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{{Host: "other.example:443", Path: "/user/**"}}, Editable: true, Status: "active"})
					}
					installed = true
					return "", nil
				default:
					return "", fmt.Errorf("unexpected SSH command %q", command.Args[len(command.Args)-1])
				}
			}
			fixture.rebuildClients()
			request := installationRequest{
				Scope: "scope", Socket: "/tmp/policy.sock", Sandbox: sbx.Sandbox{ID: "sandbox-1", Name: "demo", Status: "running"},
				Authorized: true, Current: func() bool { return current },
			}
			version, err := fixture.service.installHerdr(ctx, request)
			if scenario == "success" || scenario == "concurrent user rule" || scenario == "existing access" {
				if err != nil || version != "herdr 0.9.3" {
					t.Fatalf("version=%q err=%v", version, err)
				}
				expectedRules := 1
				if scenario == "concurrent user rule" || scenario == "existing access" {
					expectedRules = 2
				}
				if len(policy.rules) != expectedRules || policy.rules[0].ID != "user-rule" {
					t.Fatalf("temporary install changed policy: %#v", policy.rules)
				}
				status, err := store.ReadStatusStore()
				if err != nil || len(status.Installations) != 0 {
					t.Fatalf("cleanup journal=%#v err=%v", status.Installations, err)
				}
				expectedCreates, expectedRemovals := 4, 4
				if scenario == "existing access" {
					expectedCreates, expectedRemovals = 3, 3
				}
				if policy.created != expectedCreates || policy.removed != expectedRemovals || statusChecks != 1 {
					t.Fatalf("created=%d removed=%d status checks=%d", policy.created, policy.removed, statusChecks)
				}
				return
			}
			if err == nil || version != "" {
				t.Fatalf("expected pending cleanup, got version=%q err=%v", version, err)
			}
			status, readErr := store.ReadStatusStore()
			pending := scenario == "cleanup failure" || scenario == "ambiguous acknowledgement" || scenario == "modified rule"
			wantPending := 0
			if pending {
				wantPending = 1
			}
			if readErr != nil || len(status.Installations) != wantPending {
				t.Fatalf("cleanup intent=%#v err=%v", status.Installations, readErr)
			}
			if scenario == "ambiguous acknowledgement" {
				if policy.created != 0 || len(policy.rules) != 2 || policy.rules[1].ID != "ambiguous-rule" {
					t.Fatalf("ambiguous outcome changed: created=%d rules=%#v", policy.created, policy.rules)
				}
				if err := fixture.service.RecoverPendingPolicy("scope", "/tmp/policy.sock"); err == nil {
					t.Fatal("ambiguous rule was treated as owned")
				}
				if policy.rules[1].ID != "ambiguous-rule" {
					t.Fatal("recovery removed a rule without an acknowledged ID")
				}
				return
			}
			if scenario == "modified rule" {
				if policy.created != 4 || len(policy.rules) != 2 || policy.rules[1].ID != "owned-1" || policy.rules[1].Targets[0].Path != "/user-change/**" {
					t.Fatalf("modified rule was removed or changed: %#v", policy.rules)
				}
				if err := fixture.service.RecoverPendingPolicy("scope", "/tmp/policy.sock"); err == nil {
					t.Fatal("modified rule was treated as safe to remove")
				}
				if policy.rules[1].Targets[0].Path != "/user-change/**" {
					t.Fatal("recovery removed a modified policy rule")
				}
				return
			}
			if scenario == "cleanup failure" && len(policy.rules) != 5 {
				t.Fatalf("failed cleanup should retain its rules: %#v", policy.rules)
			}
			if scenario == "cleanup failure" {
				policy.failRemovals = false
				fixture.rebuildClients() // A new service resumes the saved cleanup journal.
				if err := fixture.service.RecoverPendingPolicy("scope", "/tmp/policy.sock"); err != nil {
					t.Fatal(err)
				}
			}
			expectedRules := 1
			if scenario == "concurrent user rule" {
				expectedRules = 2
			}
			if len(policy.rules) != expectedRules || policy.rules[0].ID != "user-rule" {
				t.Fatalf("recovery changed user policy: %#v", policy.rules)
			}
			if scenario != "modified rule" {
				status, err = store.ReadStatusStore()
				if err != nil || len(status.Installations) != 0 {
					t.Fatalf("recovered journal=%#v err=%v", status.Installations, err)
				}
			}
		})
	}
}

func TestOrdinaryPreparationNeverTouchesSandboxPolicy(t *testing.T) {
	fixture := newRegistrationFixture(t, "[]")
	policy := &policyFixture{}
	fixture.transport = policy
	fixture.runner.Execute = func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "herdr" && strings.Join(command.Args, " ") == "--version" {
			return "herdr 0.9.3", nil
		}
		if command.Name == "ssh" && len(command.Args) > 0 && command.Args[len(command.Args)-1] == "uname -sm" {
			return "Linux x86_64", nil
		}
		return "", fmt.Errorf("unexpected command %s %v", command.Name, command.Args)
	}
	fixture.rebuildClients()
	fixture.service.releaseTransport = releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("forbidden")), Request: request}, nil
	})
	_, err := fixture.service.installHerdr(context.Background(), installationRequest{Sandbox: sbx.Sandbox{ID: "sandbox-1", Name: "demo"}, Current: func() bool { return true }})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected ordinary host-download failure, got %v", err)
	}
	if policy.requests != 0 {
		t.Fatalf("ordinary preparation made %d policy requests", policy.requests)
	}
}

func TestTemporaryInstallationCrashRecovery(t *testing.T) {
	if os.Getenv("HERDR_POLICY_CRASH_HELPER") != "" {
		runPolicyCrashHelper(t)
		return
	}
	root := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("HERDR_BIN_PATH", "herdr")
	rulesPath := filepath.Join(root, "rules.json")
	initial := []sbx.PolicyRule{{ID: "user-rule", SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{{Host: "updates.example:443", Path: "/manual"}}, Methods: []string{"GET"}, Editable: true, Status: "active"}}
	if err := writePolicyRules(rulesPath, initial); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTemporaryInstallationCrashRecovery$")
	var env []string
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "HERDR") && !strings.HasPrefix(item, "XDG") && !strings.HasPrefix(item, "HOME=") && !strings.HasPrefix(item, "TMPDIR=") {
			env = append(env, item)
		}
	}
	command.Env = append(env, "HERDR_POLICY_CRASH_HELPER=1", "HERDR_POLICY_RULES="+rulesPath, "HERDR_PLUGIN_STATE_DIR="+root, "HERDR_BIN_PATH=herdr", "HOME="+root, "TMPDIR="+root)
	output, err := command.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("child did not stop during installation: %v %s", err, output)
	}
	status, err := store.ReadStatusStore()
	if err != nil || len(status.Installations) != 1 {
		t.Fatalf("crash lost cleanup journal: %#v err=%v", status.Installations, err)
	}
	var attempt store.PolicyAttempt
	for _, attempt = range status.Installations {
		break
	}
	if len(attempt.Owned) != len(installerAccess) || attempt.Intent != nil {
		t.Fatalf("crash journal did not retain confirmed IDs: %#v", attempt)
	}

	runner := process.Runner{}
	transport := &filePolicyFixture{path: rulesPath}
	sandboxClient := sbx.NewWithTransport(runner, transport)
	herdrClient := herdr.New(runner)
	service := New(sandboxClient, herdrClient, precheck.New(sandboxClient, herdrClient, runner), runner)
	if err := service.RecoverPendingPolicy("scope", "/tmp/policy.sock"); err != nil {
		t.Fatal(err)
	}
	rules, err := readPolicyRules(rulesPath)
	if err != nil || len(rules) != 1 || rules[0].ID != "user-rule" {
		t.Fatalf("restart changed user rules: %#v err=%v", rules, err)
	}
	status, err = store.ReadStatusStore()
	if err != nil || len(status.Installations) != 0 {
		t.Fatalf("restart left cleanup journal: %#v err=%v", status.Installations, err)
	}
}

func runPolicyCrashHelper(t *testing.T) {
	rulesPath := os.Getenv("HERDR_POLICY_RULES")
	runner := process.Runner{Execute: func(_ context.Context, command process.Command) (string, error) {
		if command.Name == "ssh" && len(command.Args) > 0 && command.Args[len(command.Args)-1] == remoteInstallCommand() {
			os.Exit(77)
		}
		return "", fmt.Errorf("unexpected child process command: %s %v", command.Name, command.Args)
	}}
	sandboxClient := sbx.NewWithTransport(runner, &filePolicyFixture{path: rulesPath})
	herdrClient := herdr.New(runner)
	service := New(sandboxClient, herdrClient, precheck.New(sandboxClient, herdrClient, runner), runner)
	_, _ = service.installHerdr(context.Background(), installationRequest{Scope: "scope", Socket: "/tmp/policy.sock", Sandbox: sbx.Sandbox{ID: "sandbox-1", Name: "demo"}, Authorized: true, Current: func() bool { return true }})
	t.Fatal("crash helper unexpectedly completed")
}

type filePolicyFixture struct{ path string }

func (f *filePolicyFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	status, body := http.StatusOK, "{}"
	if request.URL.Path != "/policy/network/rules" {
		status, body = http.StatusNotFound, "{}"
	} else if request.Method == http.MethodGet {
		rules, err := readPolicyRules(f.path)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(struct {
			Rules []sbx.PolicyRule `json:"rules"`
		}{rules})
		body = string(encoded)
	} else if request.Method == http.MethodPost {
		body = f.mutate(request)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func (f *filePolicyFixture) mutate(request *http.Request) string {
	var envelope struct {
		Actions []struct {
			Action    string   `json:"action"`
			SandboxID string   `json:"sandbox_id"`
			Resources []string `json:"resources"`
			Methods   []string `json:"methods"`
			Path      string   `json:"path"`
			ID        string   `json:"id"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil || len(envelope.Actions) != 1 {
		return `{"results":[]}`
	}
	action := envelope.Actions[0]
	rules, err := readPolicyRules(f.path)
	if err != nil {
		return `{"results":[]}`
	}
	switch action.Action {
	case "http-allow":
		id := fmt.Sprintf("crash-owned-%d", len(rules))
		rules = append(rules, sbx.PolicyRule{ID: id, SandboxID: action.SandboxID, Scope: "sandbox:" + action.SandboxID, Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{{Host: action.Resources[0], Path: action.Path}}, Methods: action.Methods, Editable: true, Status: "active"})
		if writePolicyRules(f.path, rules) != nil {
			return `{"results":[]}`
		}
		return `{"results":[{"action":"http-allow","sandbox_id":"demo","created":[{"rule_id":"` + id + `"}]}]}`
	case "remove-id":
		filtered := rules[:0]
		for _, rule := range rules {
			if rule.ID != action.ID {
				filtered = append(filtered, rule)
			}
		}
		if writePolicyRules(f.path, filtered) != nil {
			return `{"results":[]}`
		}
		return `{"results":[{"action":"remove-id","sandbox_id":"demo"}]}`
	default:
		return `{"results":[]}`
	}
}

func writePolicyRules(path string, rules []sbx.PolicyRule) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func readPolicyRules(path string) ([]sbx.PolicyRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules []sbx.PolicyRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

type policyFixture struct {
	scenario     string
	rules        []sbx.PolicyRule
	created      int
	removed      int
	failRemovals bool
	requests     int
}

func (p *policyFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	status, body := http.StatusOK, "{}"
	if request.URL.Path == "/policy/network/rules" {
		p.requests++
	}
	if request.URL.Path == "/sandbox" {
		body = "[]"
	} else if request.URL.Path != "/policy/network/rules" || (request.Method == http.MethodGet && request.URL.Query().Get("sandbox") != "demo") {
		status, body = http.StatusNotFound, "{}"
	} else if request.Method == http.MethodGet {
		encoded, _ := json.Marshal(struct {
			Rules []sbx.PolicyRule `json:"rules"`
		}{p.rules})
		body = string(encoded)
	} else if request.Method == http.MethodPost {
		body = p.mutate(request)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func (p *policyFixture) mutate(request *http.Request) string {
	var envelope struct {
		Actions []struct {
			Action    string   `json:"action"`
			SandboxID string   `json:"sandbox_id"`
			Resources []string `json:"resources"`
			Methods   []string `json:"methods"`
			Path      string   `json:"path"`
			ID        string   `json:"id"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil || len(envelope.Actions) != 1 {
		return `{"results":[]}`
	}
	action := envelope.Actions[0]
	if action.SandboxID != "demo" {
		return `{"results":[{"action":"` + action.Action + `","sandbox_id":"` + action.SandboxID + `","error":"wrong sandbox"}]}`
	}
	switch action.Action {
	case "http-allow":
		if len(action.Resources) != 1 || len(action.Methods) != 1 || action.Methods[0] != "GET" || action.Path == "" {
			return `{"results":[{"action":"http-allow","sandbox_id":"demo","error":"overbroad request"}]}`
		}
		access := sbx.PolicyAccess{Host: action.Resources[0], Path: action.Path}
		if p.scenario == "rejected allowance" || p.scenario == "blocked" {
			detail := "allowance rejected by policy service"
			if p.scenario == "blocked" {
				detail = "sandbox not found"
			}
			return `{"results":[{"action":"http-allow","sandbox_id":"demo","error":"` + detail + `"}]}`
		}
		if p.scenario == "partial error" {
			p.created++
			id := fmt.Sprintf("owned-%d", p.created)
			p.rules = append(p.rules, sbx.PolicyRule{ID: id, SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{access}, Methods: []string{"GET"}, Editable: true, Status: "active"})
			return `{"results":[{"action":"http-allow","sandbox_id":"demo","created":[{"rule_id":"` + id + `"}],"error":"injected partial result failure"}]}`
		}
		if p.scenario == "ambiguous acknowledgement" {
			p.rules = append(p.rules, sbx.PolicyRule{ID: "ambiguous-rule", SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{access}, Methods: []string{"GET"}, Editable: true, Status: "active"})
			return `{"results":[{"action":"http-allow","sandbox_id":"demo"}]}`
		}
		p.created++
		id := fmt.Sprintf("owned-%d", p.created)
		p.rules = append(p.rules, sbx.PolicyRule{ID: id, SandboxID: "demo", Scope: "sandbox:demo", Decision: "allow", ResourceType: "http", Targets: []sbx.PolicyAccess{access}, Methods: []string{"GET"}, Editable: true, Status: "active"})
		return `{"results":[{"action":"http-allow","sandbox_id":"demo","created":[{"rule_id":"` + id + `"}] }]}`
	case "remove-id":
		if action.ID == "user-rule" || action.ID == "ambiguous-rule" {
			return `{"results":[{"action":"remove-id","sandbox_id":"demo","error":"must preserve user rule"}]}`
		}
		if p.failRemovals {
			return `{"results":[{"action":"remove-id","sandbox_id":"demo","error":"injected removal failure"}]}`
		}
		p.removed++
		for i := range p.rules {
			if p.rules[i].ID == action.ID {
				p.rules = append(p.rules[:i], p.rules[i+1:]...)
				break
			}
		}
		return `{"results":[{"action":"remove-id","sandbox_id":"demo"}]}`
	default:
		return `{"results":[]}`
	}
}

func TestPolicyCleanupRequiresSavedSelector(t *testing.T) {
	service := &Service{}
	attempt := store.PolicyAttempt{Scope: "scope", Sandbox: sbx.Sandbox{ID: "sandbox-id", Name: "human-name"}}
	err := service.cleanupPolicy("/tmp/policy.sock", attempt)
	var pending *installationError
	if !errors.As(err, &pending) || pending.State != "cleanup pending" || !strings.Contains(pending.Detail, "saved sandbox selector is missing") {
		t.Fatalf("missing selector result: %v", err)
	}
}

func TestPolicyAttemptRetainsOriginalSelectorAndLowercaseAccessSchema(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	attempt := store.PolicyAttempt{Scope: "scope", Sandbox: sbx.Sandbox{ID: "sandbox-id", Name: "human-name"}, PolicySandbox: "human-name", Intent: &sbx.PolicyAccess{Host: "herdr.dev:443", Path: "/install.sh"}}
	if err := store.UpdateStatusStore(func(status *store.StatusStore) {
		status.Installations[store.StatusKey("scope", "sandbox-id")] = attempt
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "state.json.status"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"host":"herdr.dev:443"`) || !strings.Contains(string(data), `"path":"/install.sh"`) || strings.Contains(string(data), `"Host"`) || strings.Contains(string(data), `"Path"`) {
		t.Fatalf("unexpected policy access schema: %s", data)
	}
}
