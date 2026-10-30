package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) ListNotificationRules(ctx context.Context) ([]client.NotificationRule, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListNotificationRules(ctx)
}

func (api configuredAPI) UpdateNotificationRule(ctx context.Context, input client.UpdateNotificationRuleInput) (client.NotificationRule, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.NotificationRule{}, err
	}
	return c.UpdateNotificationRule(ctx, input)
}
