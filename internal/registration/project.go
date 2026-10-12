package registration

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/lanwen/herdr-sbx/internal/guestplugins"
	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// Project setup is part of registration, including upgrades of already owned
// machines. Its failure must not discard ownership or add another machine.
func (s *Service) projectReadiness(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, known bool, current func() bool) result {
	outcome := s.readinessResult(ctx, sb.Name, item.ProfileID, known, current)
	if outcome.Status != "ready" {
		return outcome
	}
	if !item.ProjectWorkspaceDone && (sb.Workspace != "" || item.ProjectWorkspacePending != "") {
		if err := s.prepareProjectWorkspace(ctx, scope, st, sb, item, current); err != nil {
			return result{sb.Name, "workspace setup required", "machine is registered; project workspace: " + err.Error()}
		}
	}
	if err := guestplugins.Apply(ctx, s.herdr, s.releaseTransport, item.Target, current); err != nil {
		return result{sb.Name, "plugin setup required", err.Error()}
	}

	return outcome
}

func (s *Service) prepareProjectWorkspace(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, item *store.Tracked, current func() bool) error {
	checkCurrent := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == nil || !current() {
			return errors.New("sandbox identity could not be verified")
		}
		return nil
	}
	save := func() error {
		if err := replaceTracked(st, scope, *item); err != nil {
			return err
		}
		return store.SaveState(*st)
	}
	complete := func() error {
		if err := checkCurrent(); err != nil {
			return err
		}
		item.ProjectWorkspaceDone = true
		item.ProjectWorkspacePending = ""
		return save()
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	cwd := item.ProjectWorkspacePending
	if cwd == "" {
		// exec defaults to the sandbox's primary workspace. Resolve in the guest:
		// clone workspaces need not have the host inventory path.
		output, err := s.sbx.WorkspaceDirectory(ctx, sb.Name)
		if err != nil {
			return fmt.Errorf("resolve guest directory: %s", commandError(err, output))
		}
		cwd = strings.TrimSuffix(output, "\n")
		if !validProjectDirectory(cwd) {
			return errors.New("guest returned an invalid project directory")
		}
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	panes, output, err := s.herdr.Panes(ctx, item.ProfileID)
	if err != nil {
		return fmt.Errorf("inspect existing workspaces: %s", commandError(err, output))
	}
	for _, pane := range panes {
		if pane.WorkspaceID != "" && pane.CWD == cwd {
			return complete()
		}
	}
	if item.ProjectWorkspacePending != "" {
		return errors.New("previous creation is unresolved; inspect remote workspaces before retrying; no additional workspace was created")
	}
	if err := checkCurrent(); err != nil {
		return err
	}
	item.ProjectWorkspacePending = cwd
	if err := save(); err != nil {
		return err
	}
	if err := checkCurrent(); err != nil {
		item.ProjectWorkspacePending = ""
		if saveErr := save(); saveErr != nil {
			return saveErr
		}
		return err
	}
	response, output, createErr := s.herdr.CreateWorkspace(ctx, item.ProfileID, cwd, sb.Name)
	if response.Rejected != "" {
		item.ProjectWorkspacePending = ""
		if err := save(); err != nil {
			return err
		}
		return fmt.Errorf("create rejected: %s", response.Rejected)
	}
	if createErr != nil {
		return fmt.Errorf("creation outcome is uncertain: %s", commandError(createErr, output))
	}
	if response.ID == "" || response.CWD != cwd {
		return errors.New("creation returned no verified project workspace; retry registration to inspect the result")
	}
	return complete()
}

func validProjectDirectory(cwd string) bool {
	return path.IsAbs(cwd) && path.Clean(cwd) == cwd && !strings.ContainsFunc(cwd, unicode.IsControl)
}

// prepareInitialProjectWorkspace runs before machine add can bootstrap a guest
// in its SSH login directory. Persist the startup intent so failed or uncertain
// attempts must verify the same project on retry, never create another workspace.
func (s *Service) prepareInitialProjectWorkspace(ctx context.Context, scope string, st *store.State, sb sbx.Sandbox, current func() bool) (string, error) {
	cwd := ""
	for _, item := range st.Scopes[scope] {
		if item.SandboxID == sb.ID && item.ProfileID == "" {
			cwd = item.ProjectWorkspacePending
		}
	}
	if cwd == "" {
		if sb.Workspace == "" {
			return "", nil
		}
		running, err := s.herdr.GuestServerRunning(ctx, sb.Name+".sbx")
		if err != nil {
			return "", err
		}
		if running {
			return "", nil
		}
		output, err := s.sbx.WorkspaceDirectory(ctx, sb.Name)
		if err != nil {
			return "", fmt.Errorf("resolve guest directory: %s", commandError(err, output))
		}
		cwd = strings.TrimSuffix(output, "\n")
		if !validProjectDirectory(cwd) {
			return "", errors.New("guest returned an invalid project directory")
		}
		if ctx.Err() != nil || current == nil || !current() {
			return "", errors.New("sandbox changed before project startup")
		}
		st.Scopes[scope] = append(st.Scopes[scope], store.Tracked{SandboxID: sb.ID, Label: sandboxMachineLabel(sb.Name), Target: sb.Name + ".sbx", ProjectWorkspacePending: cwd})
		if err := store.SaveState(*st); err != nil {
			return "", err
		}
	}
	if ctx.Err() != nil || current == nil || !current() {
		return "", errors.New("sandbox changed before project startup")
	}
	if err := s.herdr.StartProjectServer(ctx, sb.Name+".sbx", cwd); err != nil {
		return "", err
	}
	return cwd, nil
}
