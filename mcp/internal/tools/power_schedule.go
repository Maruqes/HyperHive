package tools

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PowerScheduleAPI is read-only: changing the schedule powers off the whole
// fabric and stays in the UI.
type PowerScheduleAPI interface {
	GetPowerSchedule(context.Context) (client.PowerScheduleView, error)
}

type powerScheduleOutput struct {
	Result client.PowerScheduleView `json:"result"`
}

func defineGetPowerSchedule(api PowerScheduleAPI) ToolDefinition {
	readOnly := false
	tool := &mcp.Tool{
		Name:        "get_power_schedule",
		Description: "Read the cluster-wide power schedule (enabled, start/stop HH:MM in the master's timezone), the next off window as UTC epoch seconds (slaves stop at stopUnix, the master host at masterStopUnix, all wake at wakeUnix) and each host's last apply status (job id, error, timestamps). Calls GET /power-schedule. Takes no arguments. Read-only; the schedule can only be changed in the HyperHive UI.",
		Meta:        mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": client.PowerSchedulePath}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &readOnly, IdempotentHint: true},
	}
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, powerScheduleOutput, error) {
		view, err := api.GetPowerSchedule(ctx)
		return nil, powerScheduleOutput{view}, err
	})
}

// RegisterPowerSchedule is the complete tool and safety catalog for this area.
func RegisterPowerSchedule(server *Registry, api PowerScheduleAPI) {
	server = server.Area("power_schedule")
	RegisterTool(server, "get_power_schedule", defineGetPowerSchedule(api), safety.Low)
}
