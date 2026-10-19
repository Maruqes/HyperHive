package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

const SPAPortsPath = "/spa/"

// SPAObject retains the complete safe API response and exact JSON numbers.
type SPAObject map[string]any

func spaContainsSecret(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if strings.Contains(strings.ToLower(key), "password") || spaContainsSecret(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if spaContainsSecret(child) {
				return true
			}
		}
	}
	return false
}

func (c *Client) spaRead(ctx context.Context, path, arrayKey string) (SPAObject, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result SPAObject
	if decoder.Decode(&result) != nil || result == nil {
		return nil, errors.New("HyperHive API returned an invalid SPA object")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("HyperHive API returned trailing SPA data")
	}
	items, ok := result[arrayKey].([]any)
	if !ok {
		return nil, errors.New("HyperHive API returned an invalid SPA list")
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("HyperHive API returned an invalid SPA entry")
		}
		if arrayKey == "spa_ports" {
			port, ok := entry["port"].(json.Number)
			if !ok {
				return nil, errors.New("HyperHive API returned an invalid SPA port")
			}
			number, err := port.Int64()
			if err != nil || number < 1 || number > 65535 {
				return nil, errors.New("HyperHive API returned an invalid SPA port")
			}
			if _, ok := entry["description"].(string); !ok {
				return nil, errors.New("HyperHive API returned an invalid SPA description")
			}
			if _, ok := entry["created_at"].(string); !ok {
				return nil, errors.New("HyperHive API returned an invalid SPA timestamp")
			}
		} else {
			if _, ok := entry["ip"].(string); !ok {
				return nil, errors.New("HyperHive API returned an invalid SPA IP")
			}
			remaining, ok := entry["remaining_seconds"].(json.Number)
			if !ok {
				return nil, errors.New("HyperHive API returned an invalid SPA timeout")
			}
			if _, err := remaining.Int64(); err != nil {
				return nil, errors.New("HyperHive API returned an invalid SPA timeout")
			}
		}
	}
	if spaContainsSecret(map[string]any(result)) {
		return nil, errors.New("HyperHive API returned unexpected sensitive SPA fields")
	}
	return result, nil
}

func (c *Client) ListSPAPorts(ctx context.Context) (SPAObject, error) {
	return c.spaRead(ctx, SPAPortsPath, "spa_ports")
}

// SPAPortInput identifies a configured SPA port; it never accepts a password.
type SPAPortInput struct {
	Port int `json:"port" jsonschema:"Configured SPA port from 1 to 65535"`
}

func ValidateSPAPort(input SPAPortInput) error {
	if input.Port < 1 || input.Port > 65535 {
		return errors.New("port must be an integer from 1 to 65535")
	}
	return nil
}

func (c *Client) ListSPAAllows(ctx context.Context, input SPAPortInput) (SPAObject, error) {
	if err := ValidateSPAPort(input); err != nil {
		return nil, err
	}
	result, err := c.spaRead(ctx, "/spa/allow/"+strconv.Itoa(input.Port), "allows")
	if err != nil {
		return nil, err
	}
	if result["port"] != json.Number(strconv.Itoa(input.Port)) {
		return nil, errors.New("HyperHive API returned an inconsistent SPA port")
	}
	return result, nil
}
