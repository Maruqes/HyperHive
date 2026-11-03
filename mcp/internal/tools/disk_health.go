package tools

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DiskHealthAPI is read-only: purging a retired disk's history deletes data
// and stays in the UI.
type DiskHealthAPI interface {
	ListDiskHealth(context.Context, client.DiskHealthListInput) (client.DiskHealthList, error)
	GetDiskHealthHistory(context.Context, client.DiskHealthHistoryInput) (client.DiskHealthHistory, error)
}

type diskHealthListOutput struct {
	Result client.DiskHealthList `json:"result"`
}

type diskHealthHistoryOutput struct {
	Result client.DiskHealthHistory `json:"result"`
}

func diskHealthTool(name, path, description string) *mcp.Tool {
	readOnly := false
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Meta:        mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": path}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &readOnly, IdempotentHint: true},
	}
}

func defineListDiskHealth(api DiskHealthAPI) ToolDefinition {
	tool := diskHealthTool("list_disk_health", client.DiskHealthPath,
		"List physical disks across all hosts with their SMART health score (0-100), band (good, watch, at_risk, critical), plain-language reasons, projected wear-out date for SSD/NVMe (an estimate), latest sample and retired state. The master polls SMART every 6 hours; lastPollAt is the newest poll. Calls GET /diskhealth. Optional state: active, retired or all (default). Read-only.")
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, input client.DiskHealthListInput) (*mcp.CallToolResult, diskHealthListOutput, error) {
		list, err := api.ListDiskHealth(ctx, input)
		return nil, diskHealthListOutput{list}, err
	})
}

func defineGetDiskHealthHistory(api DiskHealthAPI) ToolDefinition {
	tool := diskHealthTool("get_disk_health_history", client.DiskHealthPath+"/{serial}",
		"Read one disk's health history by serial: the disk with its score, band and reasons, plus chart points oldest first. Points from the last 90 days are raw 6-hourly samples (resolution 6h); older points are one row per day (resolution daily) with the worst score and highest counters of that day, kept for 2 years. Calls GET /diskhealth/{serial}. Optional days limits the range. Read-only.")
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, input client.DiskHealthHistoryInput) (*mcp.CallToolResult, diskHealthHistoryOutput, error) {
		history, err := api.GetDiskHealthHistory(ctx, input)
		return nil, diskHealthHistoryOutput{history}, err
	})
}

// RegisterDiskHealth is the complete tool and safety catalog for this area.
func RegisterDiskHealth(server *Registry, api DiskHealthAPI) {
	server = server.Area("disk_health")
	RegisterTool(server, "list_disk_health", defineListDiskHealth(api), safety.Low)
	RegisterTool(server, "get_disk_health_history", defineGetDiskHealthHistory(api), safety.Low)
}
