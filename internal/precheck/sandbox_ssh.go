package precheck

import (
	"context"
	"regexp"
	"time"

	"github.com/lanwen/herdr-sbx/internal/process"
)

var sandboxSSHProxy = regexp.MustCompile(`^(?:"(?:[^"\r\n]*/)?sbx"|'(?:[^'\r\n]*/)?sbx'|(?:[^\s"']*/)?sbx)\s+ssh\s+proxy(?:\s|$)`)

func (i *Inspector) sandboxSSH() Check {
	output, err := i.commands.CombinedOutput(context.Background(), process.Command{Name: "ssh", Args: []string{"-G", "herdr-sandbox-probe.sbx"}, Timeout: 5 * time.Second})
	if err != nil {
		return Check{"Sandbox SSH", Unknown, commandError(err, output), "Run `sbx setup ssh` only if you want Docker Sandboxes to configure SSH."}
	}
	proxy := sshSetting(output, "proxycommand")
	if proxy == "" || proxy == "none" {
		return Check{"Sandbox SSH", Blocked, "no managed sandbox SSH proxy is configured", "Run `sbx setup ssh`, then check again."}
	}
	if !sandboxSSHProxy.MatchString(proxy) {
		return Check{"Sandbox SSH", Unknown, "an SSH proxy is configured but is not verified as Docker Sandboxes", "Run `sbx setup ssh` or inspect the effective SSH configuration."}
	}
	return Check{"Sandbox SSH", Ready, "managed sandbox SSH proxy is configured", ""}
}
