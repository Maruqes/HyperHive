// Package client calls the existing HyperHive API without importing master services.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 16 << 20

type Config struct {
	BaseURL string
	Token   string
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("HYPERHIVE_API_URL must be an absolute http(s) URL without credentials, query or fragment")
	}
	token := strings.TrimSpace(cfg.Token)
	if len(token) >= 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" || strings.EqualFold(token, "Bearer") || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("MCP_TOKEN must contain a fixed MCP key")
	}
	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		token:   token,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// Do not forward credentials or silently follow a login redirect.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// ListProxies preserves the API object's fields, including metadata and nested
// locations. HyperHive may encode a nil proxy slice as null; expose it as [].
func (c *Client) ListProxies(ctx context.Context) ([]map[string]any, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, ListProxiesPath, &raw); err != nil {
		return nil, err
	}
	var proxies []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&proxies) != nil {
		return nil, errors.New("HyperHive API returned invalid JSON or an unexpected response shape")
	}
	if proxies == nil {
		proxies = make([]map[string]any, 0)
	}
	for _, proxy := range proxies {
		if proxy == nil {
			return nil, errors.New("HyperHive API returned an invalid proxy object")
		}
	}
	return proxies, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	return c.request(ctx, http.MethodGet, path, nil, dst)
}

// request shares authentication, limits and safe error handling. A nil dst
// explicitly selects the empty-success contract used by reviewed API mutations.
func (c *Client) request(ctx context.Context, method, path string, body, dst any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return errors.New("could not encode HyperHive API request")
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return errors.New("could not construct HyperHive API request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("HyperHive API request interrupted: %w", ctx.Err())
		}
		// net/http errors can contain URLs. Do not expose configuration or tokens.
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("HyperHive API request timed out")
		}
		return errors.New("HyperHive API request failed; check connectivity and TLS configuration")
	}
	defer resp.Body.Close()
	decoder, special := dst.(apiResponseDecoder)
	if (resp.StatusCode < 200 || resp.StatusCode >= 300) && !(special && decoder.acceptStatus(resp.StatusCode)) {
		// JSON and plain-text error bodies can contain credentials or backend
		// configuration. Return the HTTP status without echoing those bodies.
		return fmt.Errorf("HyperHive API returned HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return errors.New("could not read HyperHive API response")
	}
	if len(data) > maxResponseBytes {
		return errors.New("HyperHive API response exceeds the 16 MiB limit")
	}
	if special {
		return decoder.decodeResponse(data, resp.Header, resp.StatusCode)
	}
	if dst == nil {
		return nil
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("HyperHive API returned an empty response; expected JSON")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return errors.New("HyperHive API returned invalid JSON or an unexpected response shape")
	}
	return nil
}

// apiResponseDecoder handles the few reviewed nonstandard API response contracts.
// It is private to this package and is never a generic MCP dispatch capability.
type apiResponseDecoder interface {
	acceptStatus(int) bool
	decodeResponse([]byte, http.Header, int) error
}
