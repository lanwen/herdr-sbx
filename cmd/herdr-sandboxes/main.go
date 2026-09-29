package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	minHerdrVersion = "0.9.1"
	minSBXVersion   = "0.45.1"
)

type level string

const (
	ready   level = "ready"
	blocked level = "blocked"
	unknown level = "unknown"
)

type check struct {
	Name   string
	Level  level
	Detail string
	Action string
}

type prerequisites struct {
	Checks []check
	Scope  string
	Socket string
}

type sandbox struct {
	ID     string
	Name   string
	Status string
}

type machine struct {
	ID      string
	Label   string
	Target  string
	Session string
}

type tracked struct {
	SandboxID string `json:"sandbox_id"`
	Label     string `json:"label"`
	Target    string `json:"target"`
	ProfileID string `json:"profile_id,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
}

type state struct {
	Version int                  `json:"version"`
	Scopes  map[string][]tracked `json:"scopes"`
}

type result struct {
	Name   string
	Status string
	Detail string
}

var run = runCommand
var inventory = sandboxInventory

func main() {
	command := "panel"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	var err error
	switch command {
	case "panel":
		err = panel(os.Stdin, os.Stdout)
	case "check":
		printChecks(os.Stdout, inspect())
	case "open-status":
		err = openPanel()
	case "reconcile":
		err = reconcile(os.Stdout)
	case "repair":
		if len(os.Args) != 3 {
			err = errors.New("usage: herdr-sandboxes repair [daemon|login|ssh]")
		} else {
			err = repair(os.Args[2], os.Stdout)
		}
	case "watch":
		err = watch(os.Stdout)
	case "open-watch":
		err = openPane("watch", "tab")
	default:
		err = fmt.Errorf("usage: herdr-sandboxes [panel|check|open-status|repair|reconcile|watch]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Docker Sandboxes:", err)
		os.Exit(1)
	}
}

func panel(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		fmt.Fprint(out, "\x1b[2J\x1b[H")
		fmt.Fprintln(out, "Docker Sandboxes")
		fmt.Fprintln(out, "")
		printChecks(out, inspect())
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Stopped sandbox registrations remain available for ordinary SSH reconnects.")
		fmt.Fprintln(out, "c  Check again (observation only)")
		fmt.Fprintln(out, "r  Register prepared running sandboxes")
		fmt.Fprintln(out, "d  Start daemon")
		fmt.Fprintln(out, "l  Sign in to Docker")
		fmt.Fprintln(out, "s  Set up sandbox SSH")
		fmt.Fprintln(out, "w  Watch sandbox lifecycle")
		fmt.Fprintln(out, "q  Close")
		fmt.Fprint(out, "> ")

		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "c", "":
			continue
		case "r":
			fmt.Fprintln(out, "")
			if err := reconcile(out); err != nil {
				fmt.Fprintln(out, "Reconciliation failed:", err)
			}
			fmt.Fprintln(out, "Press Enter to return to status.")
			if _, err := reader.ReadString('\n'); err != nil && !errors.Is(err, io.EOF) {
				return err
			}
		case "d":
			panelRepair("daemon", reader, out)
		case "l":
			panelRepair("login", reader, out)
		case "s":
			panelRepair("ssh", reader, out)
		case "w":
			return watch(out)
		case "q":
			return nil
		}
	}
}

func panelRepair(kind string, reader *bufio.Reader, out io.Writer) {
	fmt.Fprintln(out, "")
	if err := repair(kind, out); err != nil {
		fmt.Fprintln(out, "Action failed:", err)
	}
	fmt.Fprintln(out, "Press Enter to return to status.")
	_, _ = reader.ReadString('\n')
}

func printChecks(out io.Writer, p prerequisites) {
	for _, c := range p.Checks {
		fmt.Fprintf(out, "%-10s %-18s %s\n", strings.ToUpper(string(c.Level)), c.Name, c.Detail)
		if c.Action != "" {
			fmt.Fprintf(out, "           Next: %s\n", c.Action)
		}
	}
}

func inspect() prerequisites {
	checks := []check{}
	herdr := herdrBinary()
	checks = append(checks, executableCheck("Herdr", herdr, minHerdrVersion))
	sbxCheck := executableCheck("sbx", "sbx", minSBXVersion)
	checks = append(checks, sbxCheck)
	checks = append(checks, executableCheck("OpenSSH", "ssh", ""))
	checks = append(checks, sandboxCapabilityCheck(sbxCheck))

	scope := ""
	socketPath := ""
	daemonRunning := false
	daemonOutput, err := runSBX("daemon", "status", "--json")
	if err != nil {
		checks = append(checks, check{"Sandbox daemon", unknown, commandError(err, daemonOutput), "Run `sbx daemon status --json` after Docker Sandboxes is available."})
	} else {
		var daemon map[string]any
		if decodeJSON(daemonOutput, &daemon) != nil {
			checks = append(checks, check{"Sandbox daemon", unknown, "returned an unrecognised status response", "Check the Docker Sandboxes version and daemon status."})
		} else {
			socket := text(daemon["socket"])
			if socket != "" {
				socketPath = socket
				scope = scopeID(socket)
			}
			status := strings.ToLower(text(daemon["status"]))
			if status == "running" {
				daemonRunning = true
				checks = append(checks, check{"Sandbox daemon", ready, "running", ""})
			} else {
				checks = append(checks, check{"Sandbox daemon", blocked, statusOr(status, "not running"), "Use Docker Sandboxes to start the daemon, then check again."})
			}
		}
	}

	if !daemonRunning {
		checks = append(checks, check{"Docker authorization", unknown, "not checked while the sandbox daemon is stopped", "Start the daemon, then check again."})
	} else {
		if _, err := inventory(socketPath); err != nil {
			checks = append(checks, inventoryFailureChecks(err, "")...)
		} else {
			checks = append(checks, check{"Sandbox inventory", ready, "local sandbox inventory is readable", ""})
			checks = append(checks, check{"Docker authorization", unknown, "inventory access does not confirm Docker sign-in", "Sign in with `sbx login` if Docker operations require it."})
		}
	}

	sshOutput, sshErr := runShort("ssh", "-G", "herdr-sandbox-probe.sbx")
	if sshErr != nil {
		checks = append(checks, check{"Sandbox SSH", unknown, commandError(sshErr, sshOutput), "Run `sbx setup ssh` only if you want Docker Sandboxes to configure SSH."})
	} else if proxy := sshSetting(sshOutput, "proxycommand"); proxy == "" || proxy == "none" {
		checks = append(checks, check{"Sandbox SSH", blocked, "no managed sandbox SSH proxy is configured", "Run `sbx setup ssh`, then check again."})
	} else if !strings.Contains(proxy, "sbx ssh proxy") {
		checks = append(checks, check{"Sandbox SSH", unknown, "an SSH proxy is configured but is not verified as Docker Sandboxes", "Run `sbx setup ssh` or inspect the effective SSH configuration."})
	} else {
		checks = append(checks, check{"Sandbox SSH", ready, "managed sandbox SSH proxy is configured", ""})
	}
	checks = append(checks, check{"Remote readiness", unknown, "checked only during explicit registration", "Choose Register prepared running sandboxes to verify a remote Herdr server."})

	return prerequisites{Checks: checks, Scope: scope, Socket: socketPath}
}

func sandboxCapabilityCheck(sbxCheck check) check {
	if sbxCheck.Level == blocked {
		if !strings.HasPrefix(sbxCheck.Detail, "requires ") {
			return check{"Sandbox capability", blocked, "Docker Sandboxes is not available", "Install Docker Sandboxes, then check again."}
		}
		return check{"Sandbox capability", blocked, "requires sbx " + minSBXVersion + " or later", "Update Docker Sandboxes, then check again."}
	}
	if sbxCheck.Level != ready {
		return check{"Sandbox capability", unknown, "Docker Sandboxes version is unavailable", "Install or update Docker Sandboxes, then check again."}
	}
	daemonHelp, daemonErr := runShort("sbx", "daemon", "status", "--help")
	listHelp, listErr := runShort("sbx", "ls", "--help")
	if daemonErr != nil || listErr != nil || !strings.Contains(daemonHelp, "--json") || !strings.Contains(listHelp, "--json") {
		return check{"Sandbox capability", blocked, "requires JSON daemon status and sandbox inventory commands", "Update Docker Sandboxes to " + minSBXVersion + " or later, then check again."}
	}
	return check{"Sandbox capability", ready, "JSON daemon status and inventory are available", ""}
}

func inventoryFailureChecks(err error, output string) []check {
	detail := commandError(err, output)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "docker login service unavailable") || strings.Contains(lower, "not logged in") || strings.Contains(lower, "authentication required") || strings.Contains(lower, "unauthorized") {
		return []check{{"Docker authorization", blocked, detail, "Sign in with `sbx login`, then check again."}}
	}
	if strings.Contains(lower, "daemon") || strings.Contains(lower, "socket") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "backend") {
		return []check{
			{"Sandbox inventory", unknown, detail, "Check the Docker Sandboxes daemon, then check again."},
			{"Docker authorization", unknown, "not checked because the sandbox backend is unavailable", "Restore daemon access, then check again."},
		}
	}
	return []check{
		{"Sandbox inventory", unknown, detail, "Check the Docker Sandboxes backend, then check again."},
		{"Docker authorization", unknown, "not checked because sandbox inventory failed", "Check Docker authorization after inventory recovers."},
	}
}

func executableCheck(name, binary, minimum string) check {
	if _, err := exec.LookPath(binary); err != nil {
		return check{name, blocked, "not found", "Install " + name + ", then check again."}
	}
	versionArg := "--version"
	if binary == "ssh" {
		versionArg = "-V"
	}
	output, err := runShort(binary, versionArg)
	if err != nil {
		if binary == "sbx" {
			output, err = runShort(binary, "version")
		}
	}
	if err != nil {
		return check{name, unknown, commandError(err, output), "Check the installed " + name + " command."}
	}
	if binary == "ssh" {
		return check{name, ready, "available", ""}
	}
	version := versionFrom(output)
	if minimum != "" && version == "" {
		return check{name, unknown, "version was not recognised", "Install or update " + toolName(name) + ", then check again."}
	}
	if minimum != "" && compareVersion(version, minimum) < 0 {
		return check{name, blocked, "requires " + minimum + " or later; found " + version, "Update " + toolName(name) + ", then check again."}
	}
	return check{name, ready, statusOr(version, strings.TrimSpace(output)), ""}
}

func toolName(name string) string {
	if name == "sbx" {
		return "Docker Sandboxes"
	}
	return name
}

func reconcile(out io.Writer) error {
	return reconcileContext(context.Background(), out)
}

func reconcileContext(ctx context.Context, out io.Writer) error {
	return reconcileMatching(ctx, out, nil, nil)
}

func reconcileMatching(ctx context.Context, out io.Writer, match func(sandbox) bool, current func(sandbox) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := inspect()
	printChecks(out, p)
	if p.Scope == "" {
		return errors.New("cannot identify the local sandbox daemon scope")
	}
	if hasBlocked(p.Checks, "Sandbox capability") || hasBlocked(p.Checks, "Sandbox daemon") || hasBlocked(p.Checks, "Docker authorization") || hasBlocked(p.Checks, "Sandbox SSH") {
		return errors.New("fix the blocked prerequisites before registering sandboxes")
	}
	if !hasLevel(p.Checks, "Sandbox capability", ready) || !hasLevel(p.Checks, "Sandbox daemon", ready) || !hasLevel(p.Checks, "Sandbox inventory", ready) || !hasLevel(p.Checks, "Sandbox SSH", ready) {
		return errors.New("complete the prerequisite checks before registering sandboxes")
	}
	unlock, err := lockState()
	if err != nil {
		return err
	}
	defer unlock()

	sandboxes, err := inventory(p.Socket)
	if err != nil {
		return fmt.Errorf("list sandboxes: %w", err)
	}
	machineOutput, err := runShortContext(ctx, herdrBinary(), "machine", "list", "--json")
	if err != nil {
		return fmt.Errorf("list Herdr machines: %s", commandError(err, machineOutput))
	}
	machines, err := parseMachines(machineOutput)
	if err != nil {
		return err
	}

	st, err := loadState()
	if err != nil {
		return err
	}
	trackedByID := map[string]tracked{}
	for _, item := range st.Scopes[p.Scope] {
		trackedByID[item.SandboxID] = item
	}

	results := []result{}
	for _, sb := range sandboxes {
		if !strings.EqualFold(sb.Status, "running") {
			continue
		}
		if match != nil && !match(sb) {
			continue
		}
		if sb.ID == "" || sb.Name == "" {
			results = append(results, result{sb.Name, "failed", "sandbox has no stable identity"})
			continue
		}
		item, known := trackedByID[sb.ID]
		if known && item.ProfileID != "" {
			if ownedMachineByID(machines, item.ProfileID, item.Label, item.Target) != nil {
				results = append(results, readinessResult(ctx, sb.Name, item.ProfileID, true))
				continue
			}
			results = append(results, result{sb.Name, "ownership conflict", "saved machine is missing; it was not recreated"})
			continue
		}
		if known && item.Pending {
			results = append(results, result{sb.Name, "ownership conflict", "previous registration is unresolved; inspect the machine catalog before retrying"})
			continue
		}
		registrationCurrent := func() bool { return sandboxRunning(p.Socket, sb) }
		if current != nil {
			registrationCurrent = func() bool { return current(sb) }
		}
		outcome, saved := registerPreparedContext(ctx, p.Scope, &st, machines, sb, registrationCurrent)
		results = append(results, result{sb.Name, outcome.Status, outcome.Detail})
		if saved != nil {
			trackedByID[sb.ID] = *saved
		}
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "No running sandboxes to register.")
	}
	for _, item := range results {
		fmt.Fprintf(out, "%-24s %-18s %s\n", item.Name, strings.ToUpper(item.Status), item.Detail)
	}
	return nil
}

func registerPrepared(scope string, st *state, machines []machine, sb sandbox) (result, *tracked) {
	return registerPreparedContext(context.Background(), scope, st, machines, sb, nil)
}

func registerPreparedContext(ctx context.Context, scope string, st *state, machines []machine, sb sandbox, current func() bool) (result, *tracked) {
	target := sb.Name + ".sbx"
	if current != nil && !current() {
		return result{sb.Name, "stale", "sandbox changed before registration"}, nil
	}
	for _, m := range machines {
		if m.Label == sb.Name || m.Target == target {
			return result{sb.Name, "ownership conflict", "a manual or untracked machine uses this label or target"}, nil
		}
	}

	versionOutput, err := runBoundedContext(ctx, 10*time.Second, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", target, "herdr", "--version")
	if err != nil {
		return result{sb.Name, "setup required", "remote Herdr is unavailable: " + commandError(err, versionOutput)}, nil
	}
	if version := versionFrom(versionOutput); version == "" || compareVersion(version, minHerdrVersion) < 0 {
		return result{sb.Name, "setup required", "remote Herdr is missing or too old"}, nil
	}
	if current != nil && !current() {
		return result{sb.Name, "stale", "sandbox changed before registration"}, nil
	}

	pending := tracked{SandboxID: sb.ID, Label: sb.Name, Target: target, Pending: true}
	st.Scopes[scope] = append(st.Scopes[scope], pending)
	if err := saveState(*st); err != nil {
		return result{sb.Name, "failed", err.Error()}, nil
	}
	if current != nil && !current() {
		if err := removeTracked(st, scope, pending.SandboxID); err != nil || saveState(*st) != nil {
			return result{sb.Name, "ownership conflict", "sandbox changed and registration intent could not be cleared"}, &pending
		}
		return result{sb.Name, "stale", "sandbox changed before machine registration"}, nil
	}

	addOutput, addErr := runBoundedContext(ctx, 30*time.Second, herdrBinary(), "machine", "add", "--label", sb.Name, target)
	if addErr != nil {
		if deterministicAddFailure(addOutput) {
			if err := removeTracked(st, scope, pending.SandboxID); err != nil {
				return result{sb.Name, "ownership conflict", "machine add failed and its intent could not be cleared"}, &pending
			}
			if err := saveState(*st); err != nil {
				return result{sb.Name, "ownership conflict", "machine add failed and its cleared intent could not be saved"}, &pending
			}
			return addFailureResult(sb.Name, addErr, addOutput), nil
		}
		return result{sb.Name, "ownership conflict", "registration outcome is uncertain: " + commandError(addErr, addOutput)}, &pending
	}

	profileID := machineIDFromAdd(addOutput)
	if profileID == "" {
		return result{sb.Name, "ownership conflict", "registration succeeded but did not return a trusted machine ID"}, &pending
	}
	listOutput, listErr := runShortContext(ctx, herdrBinary(), "machine", "list", "--json")
	if listErr != nil {
		return result{sb.Name, "ownership conflict", "registration completed but could not inspect the returned machine ID"}, &pending
	}
	after, parseErr := parseMachines(listOutput)
	if parseErr != nil || ownedMachineByID(after, profileID, sb.Name, target) == nil {
		return result{sb.Name, "ownership conflict", "registration did not yield the returned owned machine"}, &pending
	}

	pending.ProfileID = profileID
	pending.Pending = false
	if err := replaceTracked(st, scope, pending); err != nil {
		return result{sb.Name, "ownership conflict", "machine was registered but ownership could not be saved"}, &pending
	}
	if err := saveState(*st); err != nil {
		return result{sb.Name, "ownership conflict", "machine was registered but ownership could not be saved"}, &pending
	}
	if current != nil && !current() {
		return result{sb.Name, "stale", "sandbox changed after machine registration; cleanup is pending"}, &pending
	}
	return readinessResult(ctx, sb.Name, pending.ProfileID, false), &pending
}

func sandboxRunning(socket string, expected sandbox) bool {
	sandboxes, err := inventory(socket)
	if err != nil {
		return false
	}
	for _, sb := range sandboxes {
		if sb.ID == expected.ID && sb.Name == expected.Name && strings.EqualFold(sb.Status, "running") {
			return true
		}
	}
	return false
}

func readinessResult(ctx context.Context, name, profileID string, alreadyKnown bool) result {
	statusOutput, err := runBoundedContext(ctx, 15*time.Second, herdrBinary(), "--machine", profileID, "status", "server")
	if err != nil {
		return result{name, "failed", "saved machine is not ready: " + commandError(err, statusOutput) + ". Retry registration when the remote server is available."}
	}
	if alreadyKnown {
		return result{name, "ready", "already registered and reachable"}
	}
	return result{name, "ready", "registered and reachable"}
}

func addFailureResult(name string, err error, output string) result {
	detail := commandError(err, output)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "capacity") || strings.Contains(lower, "catalog full") || strings.Contains(lower, "maximum") || strings.Contains(lower, "limit") {
		return result{name, "catalog full", "Herdr cannot save another machine: " + detail + ". Remove an unused machine, then retry."}
	}
	return result{name, "failed", "machine add failed: " + detail + ". Retry after correcting the reported problem."}
}

func replaceTracked(st *state, scope string, next tracked) error {
	for i, item := range st.Scopes[scope] {
		if item.SandboxID == next.SandboxID {
			st.Scopes[scope][i] = next
			return nil
		}
	}
	return errors.New("registration intent is missing")
}

func removeTracked(st *state, scope, sandboxID string) error {
	for i, item := range st.Scopes[scope] {
		if item.SandboxID == sandboxID {
			st.Scopes[scope] = append(st.Scopes[scope][:i], st.Scopes[scope][i+1:]...)
			return nil
		}
	}
	return errors.New("registration intent is missing")
}

func openPanel() error {
	return openPane("status", "overlay")
}

func openPane(entrypoint, placement string) error {
	plugin := os.Getenv("HERDR_PLUGIN_ID")
	if plugin == "" {
		plugin = "herdr.docker-sandboxes"
	}
	output, err := runBounded(10*time.Second, herdrBinary(), "plugin", "pane", "open", "--plugin", plugin, "--entrypoint", entrypoint, "--placement", placement, "--focus")
	if err != nil {
		return errors.New(commandError(err, output))
	}
	return nil
}

func loadState() (state, error) {
	path, err := statePath()
	if err != nil {
		return state{}, err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state{Version: 1, Scopes: map[string][]tracked{}}, nil
	}
	if err != nil {
		return state{}, err
	}
	var st state
	if err := json.Unmarshal(contents, &st); err != nil {
		return state{}, fmt.Errorf("read plugin state: %w", err)
	}
	if st.Version != 1 || st.Scopes == nil {
		return state{}, errors.New("plugin state has an unsupported format")
	}
	return st, nil
}

func saveState(st state) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func statePath() (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(dir, "herdr-docker-sandboxes")
	}
	return filepath.Join(dir, "state.json"), nil
}

func lockState() (func(), error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another reconciliation is already running")
	}
	return func() {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}

func parseSandboxes(raw string) ([]sandbox, error) {
	items, err := objects(raw, "sandboxes")
	if err != nil {
		return nil, fmt.Errorf("read sandbox inventory: %w", err)
	}
	result := make([]sandbox, 0, len(items))
	for _, item := range items {
		result = append(result, sandbox{ID: first(item, "uuid", "id", "sandbox_id"), Name: first(item, "name", "sandbox_name"), Status: first(item, "status", "state")})
	}
	return result, nil
}

func parseMachines(raw string) ([]machine, error) {
	items, err := objects(raw, "machines", "profiles")
	if err != nil {
		return nil, fmt.Errorf("read Herdr machine list: %w", err)
	}
	result := make([]machine, 0, len(items))
	for _, item := range items {
		result = append(result, machine{ID: first(item, "profile_id", "id"), Label: first(item, "label", "name"), Target: first(item, "ssh_target", "target", "host"), Session: first(item, "remote_session", "session")})
	}
	return result, nil
}

func objects(raw string, keys ...string) ([]map[string]any, error) {
	var value any
	if err := decodeJSON(raw, &value); err != nil {
		return nil, err
	}
	if obj, ok := value.(map[string]any); ok {
		if nested, ok := obj["result"]; ok {
			value = nested
		}
	}
	if obj, ok := value.(map[string]any); ok {
		for _, key := range keys {
			if nested, ok := obj[key]; ok {
				value = nested
				break
			}
		}
	}
	array, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected a list")
	}
	result := make([]map[string]any, 0, len(array))
	for _, item := range array {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("list contains a non-object")
		}
		result = append(result, obj)
	}
	return result, nil
}

func decodeJSON(raw string, destination any) error {
	objectStart := strings.Index(raw, "{")
	arrayStart := strings.Index(raw, "[")
	start := objectStart
	if start < 0 || (arrayStart >= 0 && arrayStart < start) {
		start = arrayStart
	}
	if start < 0 {
		return errors.New("no JSON value")
	}
	return json.NewDecoder(strings.NewReader(raw[start:])).Decode(destination)
}

func ownedMachineByID(machines []machine, id, label, target string) *machine {
	for _, machine := range machines {
		if machine.ID == id && machine.Label == label && machine.Target == target && usesDefaultSession(machine.Session) {
			return &machine
		}
	}
	return nil
}

func machineByID(machines []machine, id string) *machine {
	for i := range machines {
		if machines[i].ID == id {
			return &machines[i]
		}
	}
	return nil
}

func usesDefaultSession(session string) bool {
	return strings.EqualFold(session, "default")
}

func hasBlocked(checks []check, name string) bool {
	for _, c := range checks {
		if c.Name == name && c.Level == blocked {
			return true
		}
	}
	return false
}

func hasLevel(checks []check, name string, expected level) bool {
	for _, c := range checks {
		if c.Name == name && c.Level == expected {
			return true
		}
	}
	return false
}

func herdrBinary() string {
	if binary := os.Getenv("HERDR_BIN_PATH"); binary != "" {
		return binary
	}
	return "herdr"
}

func runShort(name string, args ...string) (string, error) {
	return runShortContext(context.Background(), name, args...)
}

func runShortContext(ctx context.Context, name string, args ...string) (string, error) {
	return runBoundedContext(ctx, 5*time.Second, name, args...)
}

func runSBX(args ...string) (string, error) {
	return runShort("sbx", sbxArgs(args...)...)
}

func sbxArgs(args ...string) []string {
	app := os.Getenv("HERDR_SANDBOXES_SBX_APP_NAME")
	if app != "" {
		return append([]string{"--app-name", app}, args...)
	}
	return args
}

func runBounded(timeout time.Duration, name string, args ...string) (string, error) {
	return runBoundedContext(context.Background(), timeout, name, args...)
}

func runBoundedContext(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return run(ctx, name, args...)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("timed out")
	}
	if err != nil && ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	return string(output), err
}

func commandError(err error, output string) string {
	message := strings.TrimSpace(output)
	if message == "" {
		return err.Error()
	}
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 180 {
		message = message[:177] + "..."
	}
	return message
}

var semver = regexp.MustCompile(`(?m)(?:^|\s|v)(\d+)\.(\d+)\.(\d+)(?:\s|$|[-+])`)
var machineAdd = regexp.MustCompile(`(?m)^Saved SSH machine ([^[:space:]]+)\. Remote server is ready\.$`)

func versionFrom(value string) string {
	match := semver.FindStringSubmatch(value)
	if len(match) == 4 {
		return match[1] + "." + match[2] + "." + match[3]
	}
	return ""
}

func machineIDFromAdd(output string) string {
	match := machineAdd.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func deterministicAddFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "capacity") || strings.Contains(lower, "catalog full") || strings.Contains(lower, "maximum") || strings.Contains(lower, "limit")
}

func compareVersion(left, right string) int {
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

func scopeID(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])
}

func first(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := text(item[key]); value != "" {
			return value
		}
	}
	return ""
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func sshSetting(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == key {
			return strings.Join(fields[1:], " ")
		}
	}
	return ""
}
