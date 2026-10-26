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

func installerAccess() []sbx.PolicyAccess {
	return []sbx.PolicyAccess{
		{Host: "herdr.dev:443", Path: "/install.sh"}, {Host: "herdr.dev:443", Path: "/latest.json"},
		{Host: "github.com:443", Path: "/herdrdev/herdr/releases/download/**"},
		{Host: "release-assets.githubusercontent.com:443", Path: "/github-production-release-asset/1193909050/**"},
	}
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
		if allowsRead(rule, access) {
			return true
		}
	}
	return false
}
func allowsRead(rule sbx.PolicyRule, access sbx.PolicyAccess) bool {
	if rule.Decision != "allow" || rule.ResourceType != "http" || rule.Status != "active" || !sameAccess(rule, access) {
		return false
	}
	if len(rule.Methods) == 0 {
		return true
	}
	for _, method := range rule.Methods {
		if method == "GET" || method == "ANY" {
			return true
		}
	}
	return false
}

// Cleanup has its own deadline so canceling installation still restores access.
// An unacknowledged creation is never adopted from a matching destination.
func (s *Service) cleanupPolicy(socket string, attempt store.PolicyAttempt) error {
	if err := CheckAutomaticWork(context.Background()); err != nil {
		return policyPending("Management is OFF; temporary policy cleanup remains pending")
	}
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
		stopped, cleanupErr := s.cleanupOwnedRule(ctx, socket, attempt.PolicySandbox, owned, rules)
		if stopped {
			return cleanupErr
		}
		if cleanupErr != nil {
			failures = append(failures, cleanupErr)
		}
	}

	rules, err = s.sbx.ListPolicyRules(ctx, socket, attempt.PolicySandbox)
	if err != nil {
		failures = append(failures, err)
	} else if err := uncertainPolicyIntent(rules, attempt); err != nil {
		failures = append(failures, err)
	}

	return finishPolicyCleanup(attempt, failures)
}
func finishPolicyCleanup(attempt store.PolicyAttempt, failures []error) error {
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

func uncertainPolicyIntent(rules []sbx.PolicyRule, attempt store.PolicyAttempt) error {
	if attempt.Intent == nil {
		return nil
	}
	for _, rule := range rules {
		if scopedRule(rule, attempt.PolicySandbox) && sameAccess(rule, *attempt.Intent) {
			return fmt.Errorf("creation outcome uncertain for %s%s; inspect policy for sandbox ID %s manually; matching rules are not owned", attempt.Intent.Host, attempt.Intent.Path, attempt.Sandbox.ID)
		}
	}
	return nil
}

func (s *Service) cleanupOwnedRule(ctx context.Context, socket, policySandbox string, owned store.OwnedPolicyRule, rules []sbx.PolicyRule) (bool, error) {
	found := policyRuleByID(rules, owned.ID)
	if found == nil {
		return false, nil
	}
	if !unchangedOwnedRule(*found, policySandbox, owned.Access) {
		return false, fmt.Errorf("owned rule %s changed since creation; inspect it manually", owned.ID)
	}
	if err := CheckAutomaticWork(ctx); err != nil {
		return true, policyPending("Management is OFF; temporary policy cleanup remains pending")
	}
	_, removeErr := s.sbx.RemovePolicyRule(ctx, socket, policySandbox, owned.ID)
	after, listErr := s.sbx.ListPolicyRules(ctx, socket, policySandbox)
	if listErr != nil {
		return false, listErr
	}
	if policyRuleByID(after, owned.ID) != nil {
		return false, fmt.Errorf("rule %s remains after cleanup (%v)", owned.ID, removeErr)
	}
	return false, nil
}

func policyRuleByID(rules []sbx.PolicyRule, id string) *sbx.PolicyRule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

func unchangedOwnedRule(rule sbx.PolicyRule, policySandbox string, access sbx.PolicyAccess) bool {
	return scopedRule(rule, policySandbox) && rule.Editable && rule.Decision == "allow" && rule.ResourceType == "http" && len(rule.Targets) == 1 && rule.Targets[0] == access && len(rule.Methods) == 1 && rule.Methods[0] == "GET"
}

// Activation may restore previously authorized temporary access. Refresh stays observational.
func (s *Service) recoverPendingPolicy(scope, socket string) error {
	if err := CheckAutomaticWork(context.Background()); err != nil {
		return err
	}
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
	if !validTemporaryRequest(request) {
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
	if err := s.createInstallerAccess(ctx, request, rules, &attempt); err != nil {
		return "", err
	}

	if !request.Current() {
		return "", context.Canceled
	}
	return s.prepareMissingRemote(ctx, target, request.Current, request.Progress)
}

func (s *Service) createInstallerAccess(ctx context.Context, request installationRequest, rules []sbx.PolicyRule, attempt *store.PolicyAttempt) error {
	for _, access := range installerAccess() {
		if existingAccess(rules, access) {
			continue
		}
		if err := checkGuestCurrent(ctx, request.Current); err != nil {
			return err
		}
		attempt.Intent = &access
		if err := savePolicyAttempt(*attempt); err != nil {
			return err
		}
		if err := s.createPolicyAccess(ctx, request, access, attempt); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) createPolicyAccess(ctx context.Context, request installationRequest, access sbx.PolicyAccess, attempt *store.PolicyAttempt) error {
	response, mutationErr := s.sbx.CreatePolicyAccess(ctx, request.Socket, request.Sandbox.Name, access)
	if mutationErr != nil {
		return policyPending("Temporary access creation is uncertain: " + mutationErr.Error())
	}
	if err := recordPolicyAcknowledgement(response, access, attempt); err != nil {
		return err
	}
	attempt.Intent = nil
	if err := savePolicyAttempt(*attempt); err != nil {
		return policyPending("Could not record created rules: " + err.Error())
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func validTemporaryRequest(request installationRequest) bool {
	return request.Socket != "" && request.Scope != "" && request.Sandbox.ID != "" && request.Sandbox.Name != ""
}

func recordPolicyAcknowledgement(response sbx.PolicyMutation, access sbx.PolicyAccess, attempt *store.PolicyAttempt) error {
	for _, created := range response.Created {
		if created == "" {
			return policyPending("Created policy rule has no ID")
		}
		attempt.Owned = append(attempt.Owned, store.OwnedPolicyRule{ID: created, Access: access})
	}
	for _, existing := range response.Existing {
		if existing == "" {
			return policyPending("Existing policy rule has no ID; creation outcome is uncertain")
		}
	}
	if len(response.Created)+len(response.Existing) == 0 {
		if response.Error != "" {
			// Keep intent until cleanup verifies no matching allowance remains.
			return errors.New(response.Error)
		}
		return policyPending("Policy API did not acknowledge an allowance; creation outcome is uncertain")
	}
	return nil
}
