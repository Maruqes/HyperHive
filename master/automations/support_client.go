package automations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const supportTimeout = 10 * time.Second

// Config is the Support-owned automation config; State is passed through.
type Config struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Enabled        bool            `json:"enabled"`
	Condition      string          `json:"condition"`
	Cooldown       int             `json:"cooldown_seconds"`
	For            int             `json:"for_seconds"`
	Timeout        int             `json:"timeout_seconds"`
	MaxRunsPerHour int             `json:"max_runs_per_hour"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	State          json.RawMessage `json:"state"`
}

type ConfigList struct {
	Paused      bool     `json:"paused"`
	Automations []Config `json:"automations"`
}

// SupportError is an error response from HH Support.
type SupportError struct {
	Status  int
	Message string
}

func (err *SupportError) Error() string { return err.Message }

var supportHTTP = &http.Client{Timeout: supportTimeout}

// SupportRequest calls an HH Support automation path with the proxy secret.
// body and target may be nil.
func SupportRequest(ctx context.Context, method, path string, body, target any) error {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPPORT_URL")), "/")
	secret := os.Getenv("SUPPORT_PROXY_SECRET")
	if base == "" || len(secret) < 32 {
		return &SupportError{Status: http.StatusServiceUnavailable, Message: "HH Support is not configured (SUPPORT_URL and SUPPORT_PROXY_SECRET)"}
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, supportTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("X-HH-Support-Key", secret)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := supportHTTP.Do(request)
	if err != nil {
		return &SupportError{Status: http.StatusBadGateway, Message: "HH Support is unavailable"}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read HH Support response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		var problem struct {
			Error string `json:"error"`
		}
		message := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &problem) == nil && problem.Error != "" {
			message = problem.Error
		}
		return &SupportError{Status: response.StatusCode, Message: message}
	}
	if target != nil && response.StatusCode != http.StatusNoContent {
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("decode HH Support response: %w", err)
		}
	}
	return nil
}

func ListConfigs(ctx context.Context) (ConfigList, error) {
	var list ConfigList
	err := SupportRequest(ctx, http.MethodGet, "/automations", nil, &list)
	return list, err
}

func GetConfig(ctx context.Context, id string) (Config, error) {
	var config Config
	if !ValidID(id) {
		return config, &SupportError{Status: http.StatusNotFound, Message: "not found"}
	}
	err := SupportRequest(ctx, http.MethodGet, "/automations/"+url.PathEscape(id), nil, &config)
	return config, err
}

// IsSupportNotFound reports a 404 from Support.
func IsSupportNotFound(err error) bool {
	var problem *SupportError
	return errors.As(err, &problem) && problem.Status == http.StatusNotFound
}
