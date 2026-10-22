package sbx

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LifecycleEvent is one daemon lifecycle stream record.
type LifecycleEvent struct {
	Type        string  `json:"type"`
	Action      string  `json:"action"`
	SandboxID   string  `json:"sandbox_id"`
	SandboxName string  `json:"sandbox_name"`
	Seq         *uint64 `json:"seq"`
}

// Inventory lists sandboxes through the local daemon socket.
func (c *Client) Inventory(ctx context.Context, socket string) ([]Sandbox, error) {
	if socket == "" {
		return nil, fmt.Errorf("sandbox daemon did not report a socket")
	}
	body, err := c.request(ctx, socket, http.MethodGet, "/sandbox", nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	items, err := objects(string(body), "sandboxes")
	if err != nil {
		return nil, fmt.Errorf("read sandbox inventory: %w", err)
	}
	result := make([]Sandbox, 0, len(items))
	for _, item := range items {
		workspace, err := sandboxWorkspace(item)
		if err != nil {
			return nil, fmt.Errorf("read sandbox inventory: %w", err)
		}
		result = append(result, Sandbox{
			ID:                  first(item, "uuid", "id", "sandbox_id"),
			Name:                first(item, "name", "sandbox_name"),
			Status:              first(item, "status", "state"),
			Workspace:           workspace,
			WorkspaceNeedsProbe: workspaceNeedsProbe(item),
		})
	}
	return result, nil
}

func sandboxWorkspace(item map[string]any) (string, error) {
	value, exists := item["workspace"]
	if !exists {
		return "", nil
	}
	workspace, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("sandbox workspace is not a string")
	}
	return workspace, nil
}

func workspaceNeedsProbe(item map[string]any) bool {
	raw := item["labels"]
	if raw == nil {
		return false
	}
	labels, ok := raw.(map[string]any)
	if !ok {
		return true
	}
	const prefix = "com.docker.sandbox."
	for _, key := range []string{"usesWorktree", "worktreePath", "workingDirectory"} {
		if value, exists := labels[prefix+key]; exists {
			if _, ok := value.(string); !ok {
				return true
			}
		}
	}
	worktree, _ := labels[prefix+"usesWorktree"].(string)
	worktreePath, _ := labels[prefix+"worktreePath"].(string)
	if (worktree != "" && worktree != "false") || worktreePath != "" {
		return true
	}
	workingDirectory, _ := labels[prefix+"workingDirectory"].(string)
	return workingDirectory != "" && workingDirectory != item["workspace"]
}

// ErrSandboxIdentityChanged means a verified named lookup found a different UUID.
// It does not authorize removal of an entry using that sandbox target.
var ErrSandboxIdentityChanged = errors.New("sandbox identity changed")

// SandboxAbsent proves that the sandbox name is not found. A replacement is
// an identity conflict, never permission to remove an entry using that target.
func (c *Client) SandboxAbsent(ctx context.Context, socket, name, expectedID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sandboxd/sandbox/"+url.PathEscape(name), nil)
	if err != nil {
		return false, err
	}
	response, err := c.httpClient(socket).Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return false, err
	}
	return sandboxAbsence(response.StatusCode, body, name, expectedID)
}

// ReadLifecycle streams parsed events until cancellation or a reader error.
func (c *Client) ReadLifecycle(ctx context.Context, socket string, deliver func(LifecycleEvent) bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sandboxd/events?type=sandbox.lifecycle", nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient(socket).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("sandbox event stream returned HTTP %d", response.StatusCode)
	}
	return readLifecycleLines(response.Body, deliver)
}

func (c *Client) request(ctx context.Context, socket, method, path string, body io.Reader, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, "http://sandboxd"+path, body)
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient(socket).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox daemon returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}

func (c *Client) httpClient(socket string) *http.Client {
	transport := c.transport
	if transport == nil {
		dialer := net.Dialer{}
		transport = &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}}
	}
	return &http.Client{Transport: transport}
}

func sandboxAbsence(status int, body []byte, name, expectedID string) (bool, error) {
	if status == http.StatusNotFound {
		var response struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := decodeJSON(string(body), &response); err != nil {
			return false, fmt.Errorf("sandbox daemon returned an unrecognised absence response")
		}
		if response.Code == "sandbox_not_found" || response.Message == fmt.Sprintf("sandbox %q not found", name) {
			return true, nil
		}
		return false, fmt.Errorf("sandbox daemon returned an unrecognised absence response")
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("sandbox daemon returned HTTP %d", status)
	}
	sandbox, err := parseSandbox(string(body))
	if err != nil {
		return false, err
	}
	if sandbox.Name != name {
		return false, fmt.Errorf("sandbox daemon returned a different sandbox name")
	}
	if sandbox.ID != expectedID {
		return false, fmt.Errorf("sandbox name %q now belongs to another identity; deletion is not confirmed: %w", name, ErrSandboxIdentityChanged)
	}
	return false, nil
}

func parseSandbox(raw string) (Sandbox, error) {
	var item map[string]any
	if err := decodeJSON(raw, &item); err != nil {
		return Sandbox{}, err
	}
	workspace, err := sandboxWorkspace(item)
	if err != nil {
		return Sandbox{}, err
	}
	result := Sandbox{ID: first(item, "uuid", "id", "sandbox_id"), Name: first(item, "name", "sandbox_name"), Status: first(item, "status", "state"), Workspace: workspace, WorkspaceNeedsProbe: workspaceNeedsProbe(item)}
	if result.ID == "" || result.Name == "" {
		return Sandbox{}, fmt.Errorf("sandbox daemon returned an incomplete sandbox")
	}
	return result, nil
}

func readLifecycleLines(body io.Reader, deliver func(LifecycleEvent) bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event LifecycleEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if !deliver(event) {
			return context.Canceled
		}
	}
	return scanner.Err()
}

func objects(raw string, keys ...string) ([]map[string]any, error) {
	var value any
	if err := decodeJSON(raw, &value); err != nil {
		return nil, err
	}
	if object, ok := value.(map[string]any); ok {
		if nested, ok := object["result"]; ok {
			value = nested
		}
	}
	if object, ok := value.(map[string]any); ok {
		for _, key := range keys {
			if nested, ok := object[key]; ok {
				value = nested
				break
			}
		}
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list")
	}
	result := make([]map[string]any, 0, len(array))
	for _, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("list contains a non-object")
		}
		result = append(result, object)
	}
	return result, nil
}

func first(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := text(item[key]); value != "" {
			return value
		}
	}
	return ""
}
