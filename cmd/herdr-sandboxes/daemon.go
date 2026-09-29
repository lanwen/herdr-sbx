package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type lifecycleEvent struct {
	Type        string  `json:"type"`
	Action      string  `json:"action"`
	SandboxID   string  `json:"sandbox_id"`
	SandboxName string  `json:"sandbox_name"`
	Seq         *uint64 `json:"seq"`
}

func sandboxInventory(socket string) ([]sandbox, error) {
	if socket == "" {
		return nil, fmt.Errorf("sandbox daemon did not report a socket")
	}
	body, err := daemonRequest(context.Background(), socket, "/sandbox", 5*time.Second)
	if err != nil {
		return nil, err
	}
	return parseSandboxes(string(body))
}

func daemonRequest(ctx context.Context, socket, path string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sandboxd"+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := daemonClient(socket).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox daemon returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}

func daemonClient(socket string) *http.Client {
	dialer := net.Dialer{}
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", socket)
	}}}
}

func readLifecycle(ctx context.Context, socket string, deliver func(lifecycleEvent) bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sandboxd/events?type=sandbox.lifecycle", nil)
	if err != nil {
		return err
	}
	response, err := daemonClient(socket).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("sandbox event stream returned HTTP %d", response.StatusCode)
	}
	return readLifecycleLines(response.Body, deliver)
}

func readLifecycleLines(body io.Reader, deliver func(lifecycleEvent) bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event lifecycleEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if !deliver(event) {
			return context.Canceled
		}
	}
	return scanner.Err()
}
