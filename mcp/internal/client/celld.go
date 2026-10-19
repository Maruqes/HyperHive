package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

const CelldFleetsPath = "/celld/fleets"

// CelldObject preserves complete API objects, including optional diagnostic fields.
type CelldObject map[string]any

func decodeCelldObject(data []byte, required map[string]string) (CelldObject, error) {
	var object CelldObject
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&object) != nil || object == nil {
		return nil, errors.New("HyperHive API returned an invalid Celld object")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("HyperHive API returned trailing Celld response data")
	}
	for name, kind := range required {
		value, exists := object[name]
		if !exists {
			return nil, errors.New("HyperHive API returned an incomplete Celld object")
		}
		valid := false
		switch kind {
		case "array":
			_, valid = value.([]any)
		case "object":
			_, valid = value.(map[string]any)
		case "bool":
			_, valid = value.(bool)
		case "string":
			_, valid = value.(string)
		case "number":
			_, valid = value.(json.Number)
		}
		if !valid {
			return nil, errors.New("HyperHive API returned an invalid Celld field")
		}
	}
	return object, nil
}

func (c *Client) ListCelldFleets(ctx context.Context) (CelldObject, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, CelldFleetsPath, &raw); err != nil {
		return nil, err
	}
	return decodeCelldObject(raw, map[string]string{"fleets": "array", "storage": "object", "reset_pending": "bool"})
}

const CelldHealthPath = "/celld/health"

type celldHealthResponse struct{ result CelldObject }

func (*celldHealthResponse) acceptStatus(status int) bool { return status == 503 }
func (r *celldHealthResponse) decodeResponse(data []byte, _ http.Header, status int) error {
	value, err := decodeCelldObject(data, map[string]string{"healthy": "bool", "storage": "object", "fleets": "array", "machines": "array", "checked_at": "string"})
	if err != nil {
		return err
	}
	if (status == 503 && value["healthy"] != false) || (status != 503 && value["healthy"] != true) {
		return errors.New("HyperHive API returned inconsistent Celld health status")
	}
	r.result = value
	return nil
}
func (c *Client) GetCelldHealth(ctx context.Context) (CelldObject, error) {
	var response celldHealthResponse
	if err := c.request(ctx, http.MethodGet, CelldHealthPath, nil, &response); err != nil {
		return nil, err
	}
	return response.result, nil
}

// CelldPorts is an explicit pair; omit Ports on creation for automatic allocation.
type CelldPorts struct {
	HTTP     int `json:"http" jsonschema:"HTTP port from 1 to 65535"`
	Internal int `json:"internal" jsonschema:"Distinct internal port from 1 to 65535"`
}
type CreateCelldFleetInput struct {
	Name    string      `json:"name" jsonschema:"Unique fleet name, 1 to 128 bytes without surrounding whitespace"`
	Servers []string    `json:"servers" jsonschema:"Nonempty unique server names from HyperHive inventory"`
	Ports   *CelldPorts `json:"ports,omitempty" jsonschema:"Optional explicit port pair; omit for automatic allocation"`
}

func (c *Client) celldFleetRequest(ctx context.Context, method, path string, body any) (CelldObject, error) {
	var raw json.RawMessage
	if err := c.request(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	return decodeCelldObject(raw, map[string]string{"id": "string", "name": "string", "desired_state": "string", "status": "string", "servers": "array", "ports": "object", "storage": "object", "urls": "array", "deploy": "object", "checked_at": "string"})
}
func (c *Client) CreateCelldFleet(ctx context.Context, input CreateCelldFleetInput) (CelldObject, error) {
	return c.celldFleetRequest(ctx, http.MethodPost, CelldFleetsPath, input)
}

type CelldFleetIDInput struct {
	ID string `json:"id" jsonschema:"Fleet ID returned by list_celld_fleets or create_celld_fleet"`
}

var celldFleetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func ValidateCelldFleetID(id string) error {
	if !celldFleetIDPattern.MatchString(id) {
		return errors.New("id must contain 1–64 letters, digits, underscores or hyphens and start with a letter or digit")
	}
	return nil
}
func celldFleetPath(id, suffix string) (string, error) {
	if err := ValidateCelldFleetID(id); err != nil {
		return "", err
	}
	return CelldFleetsPath + "/" + url.PathEscape(id) + suffix, nil
}

func (c *Client) StartCelldFleet(ctx context.Context, input CelldFleetIDInput) (CelldObject, error) {
	path, err := celldFleetPath(input.ID, "/start")
	if err != nil {
		return nil, err
	}
	return c.celldFleetRequest(ctx, "POST", path, nil)
}

func (c *Client) StopCelldFleet(ctx context.Context, input CelldFleetIDInput) (CelldObject, error) {
	path, err := celldFleetPath(input.ID, "/stop")
	if err != nil {
		return nil, err
	}
	return c.celldFleetRequest(ctx, "POST", path, nil)
}

func (c *Client) DeleteCelldFleet(ctx context.Context, input CelldFleetIDInput) (CelldObject, error) {
	path, err := celldFleetPath(input.ID, "")
	if err != nil {
		return nil, err
	}
	return c.celldFleetRequest(ctx, "DELETE", path, nil)
}

func (c *Client) GetCelldCredentials(ctx context.Context, input CelldFleetIDInput) (CelldObject, error) {
	path, err := celldFleetPath(input.ID, "/credentials")
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err = c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	return decodeCelldObject(raw, map[string]string{"aws_access_key_id": "string", "aws_secret_access_key": "string"})
}
