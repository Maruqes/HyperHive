package hyperhivemcp

import (
	"context"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListSPAPorts(ctx context.Context) (client.SPAObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListSPAPorts(ctx)
}

func (api configuredAPI) ListSPAAllows(ctx context.Context, input client.SPAPortInput) (client.SPAObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListSPAAllows(ctx, input)
}
