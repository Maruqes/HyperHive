package hyperhivemcp

import (
	"context"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) upstreamClient() (*client.Client, error) {
	return client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
}
func (api configuredAPI) ListUpstreams(ctx context.Context) ([]map[string]any, error) {
	c, err := api.upstreamClient()
	if err != nil {
		return nil, err
	}
	return c.ListUpstreams(ctx)
}
func (api configuredAPI) GetUpstream(ctx context.Context, in client.UpstreamID) (map[string]any, error) {
	c, err := api.upstreamClient()
	if err != nil {
		return nil, err
	}
	return c.GetUpstream(ctx, in)
}
func (api configuredAPI) GetUpstreamCapabilities(ctx context.Context) (map[string]any, error) {
	c, err := api.upstreamClient()
	if err != nil {
		return nil, err
	}
	return c.GetUpstreamCapabilities(ctx)
}
func (api configuredAPI) CreateUpstream(ctx context.Context, in client.UpstreamConfig) (map[string]any, error) {
	c, err := api.upstreamClient()
	if err != nil {
		return nil, err
	}
	return c.CreateUpstream(ctx, in)
}
func (api configuredAPI) UpdateUpstream(ctx context.Context, in client.UpstreamUpdate) (map[string]any, error) {
	c, err := api.upstreamClient()
	if err != nil {
		return nil, err
	}
	return c.UpdateUpstream(ctx, in)
}
