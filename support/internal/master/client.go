// Package master calls the HyperHive master's internal automation endpoints.
// Requests carry the shared proxy secret in X-HH-Support-Key.
package master

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrAlreadyRunning means the master refused a fire because a run of the same
// automation is still in progress.
var ErrAlreadyRunning = errors.New("automation is already running")

type FireRequest struct {
	AutomationID   string         `json:"automation_id"`
	AutomationName string         `json:"automation_name"`
	Trigger        string         `json:"trigger"`
	Condition      string         `json:"condition"`
	Snapshot       map[string]any `json:"snapshot"`
	TimeoutSeconds int            `json:"timeout_seconds"`
}

type Notification struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

type Client struct {
	base   string
	secret string
	http   *http.Client
}

func New(base, secret string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), secret: secret, http: &http.Client{Timeout: 15 * time.Second}}
}

// Fire asks the master to start a run and returns its run ID.
func (client *Client) Fire(ctx context.Context, request FireRequest) (string, error) {
	var response struct {
		RunID string `json:"run_id"`
	}
	if err := client.post(ctx, "/support-internal/automations/fire", request, &response); err != nil {
		return "", err
	}
	return response.RunID, nil
}

func (client *Client) Notify(ctx context.Context, notification Notification) error {
	return client.post(ctx, "/support-internal/automations/notify", notification, nil)
}

func (client *Client) post(ctx context.Context, path string, body, target any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-HH-Support-Key", client.secret)
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("master unreachable: %w", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode == http.StatusConflict {
		return ErrAlreadyRunning
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("master returned %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if target != nil {
		return json.Unmarshal(payload, target)
	}
	return nil
}
