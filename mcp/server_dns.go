package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListDNSAliases(ctx context.Context) (client.DNSObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListDNSAliases(ctx)
}

func (api configuredAPI) GetDNSAlias(ctx context.Context, in client.DNSAliasInput) (client.DNSObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetDNSAlias(ctx, in)
}

func (api configuredAPI) AddDNSAlias(ctx context.Context, in client.DNSAliasWriteInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.AddDNSAlias(ctx, in)
}
