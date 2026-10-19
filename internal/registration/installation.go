package registration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

// One selection point keeps registration independent of the installation route.
type installationRequest struct {
	Scope, Socket string
	Sandbox       sbx.Sandbox
	Authorized    bool
	Current       func() bool
	Progress      func(string)
}
type installationError struct{ State, Detail string }

func (e *installationError) Error() string { return e.Detail }

type installationOptions struct{ Socket, Scope, AuthorizedID string }

var installerAccess = [...]sbx.PolicyAccess{
	{Host: "herdr.dev:443", Path: "/install.sh"}, {Host: "herdr.dev:443", Path: "/latest.json"},
	{Host: "github.com:443", Path: "/herdrdev/herdr/releases/download/**"},
	{Host: "release-assets.githubusercontent.com:443", Path: "/github-production-release-asset/1193909050/**"},
}

func savePolicyAttempt(attempt store.PolicyAttempt) error {
	return store.UpdateStatusStore(func(s *store.StatusStore) {
		s.Installations[store.StatusKey(attempt.Scope, attempt.Sandbox.ID)] = attempt
	})
}
func policyPending(detail string) error { return &installationError{"cleanup pending", detail} }
func scopedRule(rule sbx.PolicyRule, id string) bool {
	return rule.SandboxID == id && rule.Scope == "sandbox:"+id
}
func sameAccess(rule sbx.PolicyRule, access sbx.PolicyAccess) bool {
	for _, target := range rule.Targets {
		if target == access {
			return true
		}
	}
	return false
}
func existingAccess(rules []sbx.PolicyRule, access sbx.PolicyAccess) bool {
	for _, rule := range rules {
		if rule.Decision == "allow" && rule.ResourceType == "http" && rule.Status == "active" && sameAccess(rule, access) {
			if len(rule.Methods) == 0 {
				return true
			}
			for _, method := range rule.Methods {
				if method == "GET" || method == "ANY" {
					return true
				}
			}
		}
	}
	return false
}

// Cleanup has its own deadline so canceling installation still restores access.
// An unacknowledged creation is never adopted from a matching destination.
func (s *Service) cleanupPolicy(socket string, attempt store.PolicyAttempt) error {
	if attempt.PolicySandbox == "" {
		return policyPending("Cannot clean up temporary access: the saved sandbox selector is missing; inspect its policy manually")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rules, err := s.sbx.ListPolicyRules(ctx, socket, attempt.PolicySandbox)
	if err != nil {
		return policyPending("Cannot inspect temporary access: " + err.Error())
	}
	var failures []error
	for _, owned := range attempt.Owned {
		id := owned.ID
		var found *sbx.PolicyRule
		for i := range rules {
			if rules[i].ID == id {
				found = &rules[i]
				break
			}
		}
		if found == nil {
			continue
		}
		if !scopedRule(*found, attempt.PolicySandbox) || !found.Editable || found.Decision != "allow" || found.ResourceType != "http" || len(found.Targets) != 1 || found.Targets[0] != owned.Access || len(found.Methods) != 1 || found.Methods[0] != "GET" {
			failures = append(failures, fmt.Errorf("owned rule %s changed since creation; inspect it manually", id))
			continue
		}
		_, removeErr := s.sbx.RemovePolicyRule(ctx, socket, attempt.PolicySandbox, id)
		after, listErr := s.sbx.ListPolicyRules(ctx, socket, attempt.PolicySandbox)
		if listErr != nil {
			failures = append(failures, listErr)
			continue
		}
		for _, rule := range after {
			if rule.ID == id {
				failures = append(failures, fmt.Errorf("rule %s remains after cleanup (%v)", id, removeErr))
				break
			}
		}
	}
	rules, err = s.sbx.ListPolicyRules(ctx, socket, attempt.PolicySandbox)
	if err != nil {
		failures = append(failures, err)
	} else if attempt.Intent != nil {
		for _, rule := range rules {
			if scopedRule(rule, attempt.PolicySandbox) && sameAccess(rule, *attempt.Intent) {
				failures = append(failures, fmt.Errorf("creation outcome uncertain for %s%s; inspect policy for sandbox ID %s manually; matching rules are not owned", attempt.Intent.Host, attempt.Intent.Path, attempt.Sandbox.ID))
				break
			}
		}
	}
	if len(failures) > 0 {
		attempt.Detail = errors.Join(failures...).Error()
		if err := savePolicyAttempt(attempt); err != nil {
			return policyPending(attempt.Detail + "; save cleanup state: " + err.Error())
		}
		return policyPending(attempt.Detail)
	}
	if err := store.UpdateStatusStore(func(s *store.StatusStore) {
		delete(s.Installations, store.StatusKey(attempt.Scope, attempt.Sandbox.ID))
	}); err != nil {
		return policyPending(err.Error())
	}
	return nil
}

// Activation may restore previously authorized temporary access. Refresh stays observational.
func (s *Service) recoverPendingPolicy(scope, socket string) error {
	status, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	pending := false
	for _, attempt := range status.Installations {
		if attempt.Scope == scope {
			pending = true
			break
		}
	}
	if !pending {
		return nil
	}
	if scope == "" || socket == "" {
		return policyPending("Cannot identify the daemon for pending cleanup")
	}
	unlock, err := store.LockState()
	if err != nil {
		return err
	}
	defer unlock()
	return s.recoverPolicy(scope, socket)
}

func (s *Service) recoverPolicy(scope, socket string) error {
	status, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	for _, attempt := range status.Installations {
		if attempt.Scope == scope {
			if err := s.cleanupPolicy(socket, attempt); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) installHerdr(ctx context.Context, request installationRequest) (version string, err error) {
	target := request.Sandbox.Name + ".sbx"
	if !request.Authorized {
		return s.prepareFromHost(ctx, target, request.Current, request.Progress)
	}
	if request.Socket == "" || request.Scope == "" || request.Sandbox.ID == "" || request.Sandbox.Name == "" {
		return "", errors.New("temporary access requires an identified local daemon and sandbox")
	}
	if request.Current == nil || !request.Current() {
		return "", context.Canceled
	}
	rules, err := s.sbx.ListPolicyRules(ctx, request.Socket, request.Sandbox.Name)
	if err != nil {
		return "", err
	}
	attempt := store.PolicyAttempt{Scope: request.Scope, Sandbox: request.Sandbox, PolicySandbox: request.Sandbox.Name, Detail: "Installation in progress; temporary network rules may remain after interruption."}
	if err = savePolicyAttempt(attempt); err != nil {
		return "", err
	}
	defer func() {
		if cleanupErr := s.cleanupPolicy(request.Socket, attempt); cleanupErr != nil {
			err = cleanupErr
			version = ""
		}
	}()
	for _, access := range installerAccess {
		if existingAccess(rules, access) {
			continue
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !request.Current() {
			return "", context.Canceled
		}
		attempt.Intent = &access
		if err = savePolicyAttempt(attempt); err != nil {
			return "", err
		}
		response, mutationErr := s.sbx.CreatePolicyAccess(ctx, request.Socket, request.Sandbox.Name, access)
		if mutationErr != nil {
			return "", policyPending("Temporary access creation is uncertain: " + mutationErr.Error())
		}
		for _, created := range response.Created {
			if created == "" {
				return "", policyPending("Created policy rule has no ID")
			}
			attempt.Owned = append(attempt.Owned, store.OwnedPolicyRule{ID: created, Access: access})
		}
		for _, existing := range response.Existing {
			if existing == "" {
				return "", policyPending("Existing policy rule has no ID; creation outcome is uncertain")
			}
		}
		if len(response.Created)+len(response.Existing) == 0 {
			if response.Error != "" {
				// Keep intent until cleanup verifies no matching allowance remains.
				return "", errors.New(response.Error)
			}
			return "", policyPending("Policy API did not acknowledge an allowance; creation outcome is uncertain")
		}
		attempt.Intent = nil
		if err = savePolicyAttempt(attempt); err != nil {
			return "", policyPending("Could not record created rules: " + err.Error())
		}
		if response.Error != "" {
			return "", errors.New(response.Error)
		}
	}
	if !request.Current() {
		return "", context.Canceled
	}
	return s.prepareMissingRemote(ctx, target, request.Current, request.Progress)
}
