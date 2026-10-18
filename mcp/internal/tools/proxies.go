package tools

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listProxiesInput struct{}

type listProxiesOutput struct {
	Proxies []map[string]any `json:"proxies" jsonschema:"Proxy hosts returned by HyperHive, preserving all API fields"`
}

// ProxyAPI is the backend capability used by the shared tool registry.
type ProxyAPI interface {
	ListProxies(context.Context) ([]map[string]any, error)
	CreateProxy(context.Context, client.ProxyConfig) error
	UpdateProxy(context.Context, client.UpdateProxyInput) error
	EnableProxy(context.Context, client.ProxyIDInput) error
	DisableProxy(context.Context, client.ProxyIDInput) error
	SetupFrontendProxy(context.Context, client.FrontendProxyInput) error
	SetupSecureFrontendProxy(context.Context, client.SecureFrontendProxyInput) error
}

// Register shares explicit feature registration between HTTP and stdio.
func Register(server *mcp.Server, api ProxyAPI) {
	mcp.AddTool(server, &mcp.Tool{
		Meta:        mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": client.ListProxiesPath}},
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
	registerCreateProxy(server, api)
	registerUpdateProxy(server, api)
	registerEnableProxy(server, api)
	registerDisableProxy(server, api)
	registerSetupFrontendProxy(server, api)
	registerSetupSecureFrontendProxy(server, api)
}
