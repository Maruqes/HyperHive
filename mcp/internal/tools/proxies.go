package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listProxiesInput struct{}

type listProxiesOutput struct {
	Proxies []map[string]any `json:"proxies" jsonschema:"Proxy hosts returned by HyperHive, preserving all API fields"`
}

// ProxyLister is the backend capability needed by the sole tool.
type ProxyLister interface {
	ListProxies(context.Context) ([]map[string]any, error)
}

// Register adds exactly one tool. Add future feature tools here explicitly.
func Register(server *mcp.Server, api ProxyLister) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_proxies",
		Description: "List all proxy hosts visible to the configured HyperHive account, including domains, forwarding targets, TLS settings, enabled state, metadata and locations. Calls GET /proxy/list. Takes no arguments.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listProxiesInput) (*mcp.CallToolResult, listProxiesOutput, error) {
		proxies, err := api.ListProxies(ctx)
		if err != nil {
			return nil, listProxiesOutput{}, err
		}
		return nil, listProxiesOutput{Proxies: proxies}, nil
	})
}
