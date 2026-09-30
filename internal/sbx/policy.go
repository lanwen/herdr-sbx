package sbx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// PolicyAccess identifies one HTTP destination allowed during guest setup.
type PolicyAccess struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

// PolicyRule is one network policy rule returned by the daemon.
type PolicyRule struct {
	ID           string         `json:"id"`
	SandboxID    string         `json:"sandbox_id"`
	Scope        string         `json:"scope"`
	Decision     string         `json:"decision"`
	ResourceType string         `json:"resource_type"`
	Targets      []PolicyAccess `json:"http_targets"`
	Methods      []string       `json:"methods"`
	Editable     bool           `json:"editable"`
	Status       string         `json:"status"`
}

// PolicyMutation reports the daemon's acknowledgement for one rule operation.
type PolicyMutation struct {
	Action    string
	SandboxID string
	Created   []string
	Existing  []string
	Error     string
}

// ListPolicyRules lists all rules for the daemon's sandbox selector.
func (c *Client) ListPolicyRules(ctx context.Context, socket, sandbox string) ([]PolicyRule, error) {
	var response struct {
		Rules *[]PolicyRule `json:"rules"`
	}
	path := "/policy/network/rules?type=all&sandbox=" + url.QueryEscape(sandbox)
	if err := c.policyRequest(ctx, socket, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if response.Rules == nil {
		return nil, fmt.Errorf("policy response omitted rules")
	}
	return *response.Rules, nil
}

// CreatePolicyAccess requests one GET rule for a host and path.
func (c *Client) CreatePolicyAccess(ctx context.Context, socket, sandbox string, access PolicyAccess) (PolicyMutation, error) {
	action := struct {
		Action    string   `json:"action"`
		SandboxID string   `json:"sandbox_id"`
		Resources []string `json:"resources,omitempty"`
		Methods   []string `json:"methods,omitempty"`
		Path      string   `json:"path,omitempty"`
	}{"http-allow", sandbox, []string{access.Host}, []string{"GET"}, access.Path}
	return c.mutatePolicy(ctx, socket, action.Action, sandbox, action)
}

// RemovePolicyRule requests removal of the specified rule ID.
func (c *Client) RemovePolicyRule(ctx context.Context, socket, sandbox, id string) (PolicyMutation, error) {
	action := struct {
		Action    string `json:"action"`
		SandboxID string `json:"sandbox_id"`
		ID        string `json:"id,omitempty"`
	}{"remove-id", sandbox, id}
	return c.mutatePolicy(ctx, socket, action.Action, sandbox, action)
}

func (c *Client) mutatePolicy(ctx context.Context, socket, name, sandbox string, action any) (PolicyMutation, error) {
	var response struct {
		Results []struct {
			Action    string `json:"action"`
			SandboxID string `json:"sandbox_id"`
			Created   []struct {
				ID string `json:"rule_id"`
			} `json:"created"`
			Existing []struct {
				ID string `json:"rule_id"`
			} `json:"existing"`
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := c.policyRequest(ctx, socket, http.MethodPost, "/policy/network/rules", struct {
		Actions []any `json:"actions"`
	}{[]any{action}}, &response); err != nil {
		return PolicyMutation{}, err
	}
	if len(response.Results) != 1 || response.Results[0].Action != name || response.Results[0].SandboxID != sandbox {
		return PolicyMutation{}, fmt.Errorf("policy mutation returned an unrecognised result")
	}
	result := response.Results[0]
	mutation := PolicyMutation{Action: result.Action, SandboxID: result.SandboxID, Error: result.Error}
	for _, id := range result.Created {
		mutation.Created = append(mutation.Created, id.ID)
	}
	for _, id := range result.Existing {
		mutation.Existing = append(mutation.Existing, id.ID)
	}
	return mutation, nil
}

func (c *Client) policyRequest(ctx context.Context, socket, method, path string, body any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://sandboxd"+path, input)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient(socket).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("policy API returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result); err != nil {
		return fmt.Errorf("invalid policy response: %w", err)
	}
	return nil
}
