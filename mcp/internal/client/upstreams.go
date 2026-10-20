package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

type UpstreamMember struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Settings string `json:"settings_string"`
}
type UpstreamOptions struct {
	HashKey string `json:"hash_key,omitempty"`
}
type UpstreamConfig struct {
	Name    string           `json:"name"`
	Context string           `json:"context,omitempty" jsonschema:"http (default) or stream; a stream pool can serve both TCP and UDP"`
	Method  string           `json:"loadbalancer_type" jsonschema:"A verified method from get_upstream_capabilities"`
	Options *UpstreamOptions `json:"loadbalancer_options,omitempty"`
	Members []UpstreamMember `json:"server_arr"`
}
type UpstreamID struct {
	ID int64 `json:"id"`
}
type UpstreamUpdate struct {
	UpstreamConfig
	ID       int64 `json:"id"`
	Revision int64 `json:"revision" jsonschema:"Expected current revision from get_upstream; stale revisions return 409"`
}

func (c *Client) ListUpstreams(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/upstream/list")
}
func (c *Client) GetUpstream(ctx context.Context, in UpstreamID) (map[string]any, error) {
	var result map[string]any
	err := c.upstreamObject(ctx, "GET", "/upstream/"+strconv.FormatInt(in.ID, 10), nil, &result)
	if err == nil && result == nil {
		err = errors.New("HyperHive API returned an invalid upstream")
	}
	return result, err
}
func (c *Client) GetUpstreamCapabilities(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	err := c.upstreamObject(ctx, "GET", "/upstream/capabilities", nil, &result)
	if err == nil && result == nil {
		err = errors.New("HyperHive API returned invalid upstream capabilities")
	}
	return result, err
}
func (c *Client) CreateUpstream(ctx context.Context, in UpstreamConfig) (map[string]any, error) {
	var result map[string]any
	err := c.upstreamObject(ctx, "POST", "/upstream/create", in, &result)
	if err == nil && result == nil {
		err = errors.New("HyperHive API returned an invalid upstream")
	}
	return result, err
}
func (c *Client) UpdateUpstream(ctx context.Context, in UpstreamUpdate) (map[string]any, error) {
	var result map[string]any
	err := c.upstreamObject(ctx, "PUT", "/upstream/"+strconv.FormatInt(in.ID, 10), in, &result)
	if err == nil && result == nil {
		err = errors.New("HyperHive API returned an invalid upstream")
	}
	return result, err
}

func (c *Client) upstreamObject(ctx context.Context, method, path string, in any, out *map[string]any) error {
	var raw json.RawMessage
	if err := c.request(ctx, method, path, in, &raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return errors.New("HyperHive API returned an invalid upstream object")
	}
	return nil
}
