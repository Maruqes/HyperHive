package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const AutomationsPath = "/automations"

var automationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type AutomationIDInput struct {
	ID string `json:"id" jsonschema:"automation ID from list_automations"`
}

type CreateAutomationInput struct {
	Name           string `json:"name" jsonschema:"display name, 1-120 characters"`
	Description    string `json:"description,omitempty"`
	Condition      string `json:"condition,omitempty" jsonschema:"Condition to run (expr over HH Support data), e.g. app('db').cpu >= 95; empty means manual only"`
	Cooldown       *int   `json:"cooldown_seconds,omitempty" jsonschema:"minimum seconds between condition runs (0-604800)"`
	For            *int   `json:"for_seconds,omitempty" jsonschema:"seconds the condition must stay true before firing (0-86400)"`
	Timeout        *int   `json:"timeout_seconds,omitempty" jsonschema:"run timeout in seconds (1-86400, default 600)"`
	MaxRunsPerHour *int   `json:"max_runs_per_hour,omitempty" jsonschema:"condition runs per hour before auto-disable (1-3600, default 10)"`
}

// UpdateAutomationInput changes only the fields that are present.
type UpdateAutomationInput struct {
	ID             string  `json:"id" jsonschema:"automation ID"`
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty" jsonschema:"true lets the condition start root runs on the master host"`
	Condition      *string `json:"condition,omitempty"`
	Cooldown       *int    `json:"cooldown_seconds,omitempty"`
	For            *int    `json:"for_seconds,omitempty"`
	Timeout        *int    `json:"timeout_seconds,omitempty"`
	MaxRunsPerHour *int    `json:"max_runs_per_hour,omitempty"`
}

type EvaluateConditionInput struct {
	Condition string `json:"condition" jsonschema:"condition to evaluate against current HH Support data without running anything"`
}

type AutomationRunsInput struct {
	AutomationID string `json:"automation_id,omitempty" jsonschema:"filter by automation; empty lists all"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-200, default 50"`
}

type AutomationRunInput struct {
	RunID string `json:"run_id" jsonschema:"run ID"`
}

type AutomationFileInput struct {
	ID   string `json:"id" jsonschema:"automation ID"`
	Path string `json:"path" jsonschema:"relative file path inside the automation, e.g. main.ts"`
}

type WriteAutomationFileInput struct {
	ID      string `json:"id" jsonschema:"automation ID"`
	Path    string `json:"path" jsonschema:"relative file path, e.g. main.ts or lib/util.ts"`
	Content string `json:"content" jsonschema:"complete new UTF-8 file content (max 1 MiB); replaces the file"`
}

// InstallAutomationPackagesInput takes no arguments.
type InstallAutomationPackagesInput struct{}

type WriteAutomationPackageInput struct {
	Content string `json:"content" jsonschema:"complete package.json content; must be a JSON object"`
}

func ValidateAutomationID(id string) error {
	if !automationIDPattern.MatchString(id) {
		return errors.New("id must be an automation ID (1-128 letters, digits, _ or -)")
	}
	return nil
}

func ValidateRunID(id string) error {
	if !automationIDPattern.MatchString(id) {
		return errors.New("run_id must be a run ID")
	}
	return nil
}

func ValidateAutomationFile(id, path string) error {
	if err := ValidateAutomationID(id); err != nil {
		return err
	}
	if path == "" || len(path) > 512 || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return errors.New("path must be a relative path inside the automation")
	}
	return nil
}

func ValidateAutomationContent(content string) error {
	if len(content) > 1<<20 || !utf8.ValidString(content) {
		return errors.New("content must be UTF-8 text up to 1 MiB")
	}
	return nil
}

func (in AutomationIDInput) Validate() error  { return ValidateAutomationID(in.ID) }
func (in AutomationRunInput) Validate() error { return ValidateRunID(in.RunID) }
func (in AutomationFileInput) Validate() error {
	return ValidateAutomationFile(in.ID, in.Path)
}
func (InstallAutomationPackagesInput) Validate() error { return nil }

func (in CreateAutomationInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 120 {
		return errors.New("name is required (maximum 120 characters)")
	}
	if len(in.Condition) > 4000 || len(in.Description) > 2000 {
		return errors.New("condition or description is too long")
	}
	return nil
}

func (in UpdateAutomationInput) Validate() error {
	if err := ValidateAutomationID(in.ID); err != nil {
		return err
	}
	if in.Name == nil && in.Description == nil && in.Enabled == nil && in.Condition == nil && in.Cooldown == nil && in.For == nil && in.Timeout == nil && in.MaxRunsPerHour == nil {
		return errors.New("at least one field to change is required")
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errors.New("name must not be blank")
	}
	return nil
}

func (in EvaluateConditionInput) Validate() error {
	if strings.TrimSpace(in.Condition) == "" || len(in.Condition) > 4000 {
		return errors.New("condition is required (maximum 4000 characters)")
	}
	return nil
}

func (in AutomationRunsInput) Validate() error {
	if in.AutomationID != "" {
		if err := ValidateAutomationID(in.AutomationID); err != nil {
			return err
		}
	}
	if in.Limit < 0 || in.Limit > 200 {
		return errors.New("limit must be between 1 and 200")
	}
	return nil
}

func (in WriteAutomationFileInput) Validate() error {
	if err := ValidateAutomationFile(in.ID, in.Path); err != nil {
		return err
	}
	return ValidateAutomationContent(in.Content)
}

func (in WriteAutomationPackageInput) Validate() error {
	var object map[string]any
	if len(in.Content) > 1<<20 || json.Unmarshal([]byte(in.Content), &object) != nil || object == nil {
		return errors.New("content must be a JSON object up to 1 MiB")
	}
	return nil
}

// automationResult keeps the API's own validation message for 400/404/409:
// these bodies are HyperHive's automation validation text, not backend config.
type automationResult struct {
	value json.RawMessage
}

func (r *automationResult) acceptStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusConflict
}

func (r *automationResult) decodeResponse(data []byte, _ http.Header, status int) error {
	if status >= 300 {
		message := strings.TrimSpace(string(data))
		if len(message) > 500 {
			message = message[:500]
		}
		return fmt.Errorf("HyperHive API returned HTTP %d: %s", status, message)
	}
	if status == http.StatusNoContent || len(strings.TrimSpace(string(data))) == 0 {
		r.value = json.RawMessage(`{}`)
		return nil
	}
	if !json.Valid(data) {
		return errors.New("HyperHive API returned invalid JSON")
	}
	r.value = append(json.RawMessage(nil), data...)
	return nil
}

func (c *Client) automationRequest(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	result := &automationResult{}
	if err := c.request(ctx, method, path, body, result); err != nil {
		return nil, err
	}
	return result.value, nil
}

func automationPath(id string, suffix string) string {
	return AutomationsPath + "/" + url.PathEscape(id) + suffix
}

func (c *Client) ListAutomations(ctx context.Context) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, AutomationsPath+"/", nil)
}

func (c *Client) GetAutomation(ctx context.Context, id string) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, automationPath(id, ""), nil)
}

func (c *Client) CreateAutomation(ctx context.Context, input CreateAutomationInput) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPost, AutomationsPath+"/", input)
}

// AutomationConfig is the replace body of PUT /automations/{id}.
type AutomationConfig struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Enabled        bool   `json:"enabled"`
	Condition      string `json:"condition"`
	Cooldown       int    `json:"cooldown_seconds"`
	For            int    `json:"for_seconds"`
	Timeout        int    `json:"timeout_seconds"`
	MaxRunsPerHour int    `json:"max_runs_per_hour"`
}

// MergeAutomation applies the present fields of input onto the current config.
func MergeAutomation(current json.RawMessage, input UpdateAutomationInput) (AutomationConfig, error) {
	var config AutomationConfig
	if err := json.Unmarshal(current, &config); err != nil {
		return config, errors.New("HyperHive API returned an invalid automation")
	}
	if input.Name != nil {
		config.Name = *input.Name
	}
	if input.Description != nil {
		config.Description = *input.Description
	}
	if input.Enabled != nil {
		config.Enabled = *input.Enabled
	}
	if input.Condition != nil {
		config.Condition = *input.Condition
	}
	if input.Cooldown != nil {
		config.Cooldown = *input.Cooldown
	}
	if input.For != nil {
		config.For = *input.For
	}
	if input.Timeout != nil {
		config.Timeout = *input.Timeout
	}
	if input.MaxRunsPerHour != nil {
		config.MaxRunsPerHour = *input.MaxRunsPerHour
	}
	return config, nil
}

func (c *Client) ReplaceAutomation(ctx context.Context, id string, config AutomationConfig) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPut, automationPath(id, ""), config)
}

func (c *Client) EvaluateAutomationCondition(ctx context.Context, input EvaluateConditionInput) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPost, AutomationsPath+"/evaluate", input)
}

func (c *Client) RunAutomation(ctx context.Context, id string) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPost, automationPath(id, "/run"), nil)
}

func (c *Client) ListAutomationRuns(ctx context.Context, input AutomationRunsInput) (json.RawMessage, error) {
	query := url.Values{}
	if input.AutomationID != "" {
		query.Set("automation_id", input.AutomationID)
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	path := AutomationsPath + "/runs"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.automationRequest(ctx, http.MethodGet, path, nil)
}

func (c *Client) GetAutomationRun(ctx context.Context, runID string) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, AutomationsPath+"/runs/"+url.PathEscape(runID), nil)
}

func (c *Client) ListAutomationFiles(ctx context.Context, id string) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, automationPath(id, "/files"), nil)
}

func (c *Client) ReadAutomationFile(ctx context.Context, input AutomationFileInput) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, automationPath(input.ID, "/files/content?path="+url.QueryEscape(input.Path)), nil)
}

func (c *Client) WriteAutomationFile(ctx context.Context, input WriteAutomationFileInput) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPut, automationPath(input.ID, "/files/content?path="+url.QueryEscape(input.Path)), map[string]string{"content": input.Content})
}

func (c *Client) ReadAutomationPackage(ctx context.Context) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodGet, AutomationsPath+"/package", nil)
}

func (c *Client) WriteAutomationPackage(ctx context.Context, input WriteAutomationPackageInput) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPut, AutomationsPath+"/package", input)
}

func (c *Client) InstallAutomationPackages(ctx context.Context) (json.RawMessage, error) {
	return c.automationRequest(ctx, http.MethodPost, AutomationsPath+"/package/install", nil)
}
