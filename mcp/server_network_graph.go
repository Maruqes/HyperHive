package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) GetNetworkGraph(ctx context.Context) (client.NetworkGraph, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.NetworkGraph{}, err
	}
	return c.GetNetworkGraph(ctx)
}
