package hyperhivemcp

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
)

func (api configuredAPI) GetPowerSchedule(ctx context.Context) (client.PowerScheduleView, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.PowerScheduleView{}, err
	}
	return c.GetPowerSchedule(ctx)
}
