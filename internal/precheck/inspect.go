package precheck

import (
	"context"
	"os/exec"
	"strings"

	"github.com/lanwen/herdr-sbx/internal/herdr"
	"github.com/lanwen/herdr-sbx/internal/process"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/lanwen/herdr-sbx/internal/version"
)

const (
	minHerdrVersion = herdr.MinimumVersion
	minSBXVersion   = "0.45.1"
)

// Level classifies one prerequisite observation.
type Level string

const (
	Ready   Level = "ready"
	Blocked Level = "blocked"
	Unknown Level = "unknown"
)

// Check is one user-visible prerequisite observation.
type Check struct {
	Name   string
	Level  Level
	Detail string
	Action string
}

// Result is the ordered passive prerequisite report and its daemon identity.
type Result struct {
	Checks []Check
	Scope  string
	Socket string
}

// Inspector reads prerequisites through operation-specific clients.
type Inspector struct {
	sandboxes *sbx.Client
	herdr     *herdr.Client
	commands  process.Runner
	inventory func(context.Context, string) ([]sbx.Sandbox, error)
}

// New creates an inspector with explicit transport clients and process runner.
func New(sandboxes *sbx.Client, herdrClient *herdr.Client, commands process.Runner) *Inspector {
	return NewWithInventory(sandboxes, herdrClient, commands, sandboxes.Inventory)
}

// NewWithInventory supplies a local inventory adapter for tests.
func NewWithInventory(sandboxes *sbx.Client, herdrClient *herdr.Client, commands process.Runner, inventory func(context.Context, string) ([]sbx.Sandbox, error)) *Inspector {
	return &Inspector{sandboxes: sandboxes, herdr: herdrClient, commands: commands, inventory: inventory}
}

// Inspect performs the ten checks in the display order. It does not start a
// daemon, sign in, alter policy, prepare a guest, or register a machine.
func (i *Inspector) Inspect() Result {
	checks := []Check{
		i.herdrBinary(),
		i.sbxBinary(),
		i.sshBinary(),
	}
	checks = append(checks, i.sbxCapability(checks[1]))
	daemon, daemonRunning, daemonCheck := i.daemonStatus()
	checks = append(checks, daemonCheck)
	scope := ""
	if daemon.Socket != "" {
		scope = store.ScopeID(daemon.Socket)
	}
	if !daemonRunning {
		checks = append(checks, Check{"Docker authorization", Unknown, "not checked while the sandbox daemon is stopped", "Start the daemon, then check again."})
	} else {
		inventoryChecks, inventoryReady := i.sandboxInventory(daemon.Socket)
		checks = append(checks, inventoryChecks...)
		if inventoryReady {
			checks = append(checks, i.dockerAuthorization())
		}
	}
	checks = append(checks, i.registrationOwnership(scope)...)
	checks = append(checks, i.sandboxSSH())
	checks = append(checks, i.remoteReadiness())
	return Result{Checks: checks, Scope: scope, Socket: daemon.Socket}
}

func (i *Inspector) executable(name, binary, minimum string, versionFunc func(context.Context) (string, error)) Check {
	if _, err := exec.LookPath(binary); err != nil {
		return Check{name, Blocked, "not found", "Install " + name + ", then check again."}
	}
	output, err := versionFunc(context.Background())
	if err != nil {
		return Check{name, Unknown, commandError(err, output), "Check the installed " + name + " command."}
	}
	if name == "OpenSSH" {
		return Check{name, Ready, "available", ""}
	}
	installedVersion := version.Parse(output)
	if minimum != "" && installedVersion == "" {
		return Check{name, Unknown, "version was not recognised", "Install or update " + toolName(name) + ", then check again."}
	}
	if minimum != "" && version.Compare(installedVersion, minimum) < 0 {
		return Check{name, Blocked, "requires " + minimum + " or later; found " + installedVersion, "Update " + toolName(name) + ", then check again."}
	}
	return Check{name, Ready, statusOr(installedVersion, strings.TrimSpace(output)), ""}
}

func toolName(name string) string {
	if name == "sbx" {
		return "Docker Sandboxes"
	}
	return name
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

func statusOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
