package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListDiskHealth(ctx context.Context, input client.DiskHealthListInput) (client.DiskHealthList, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.DiskHealthList{}, err
	}
	return c.ListDiskHealth(ctx, input)
}

func (api configuredAPI) GetDiskHealthHistory(ctx context.Context, input client.DiskHealthHistoryInput) (client.DiskHealthHistory, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.DiskHealthHistory{}, err
	}
	return c.GetDiskHealthHistory(ctx, input)
}
