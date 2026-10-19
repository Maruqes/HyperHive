package hyperhivemcp

import (
	"context"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListISOs(ctx context.Context) ([]client.ISOObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListISOs(ctx)
}

func (api configuredAPI) StartISODownload(ctx context.Context, input client.StartISODownloadInput) (client.ISOAcceptance, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.StartISODownload(ctx, input)
}
