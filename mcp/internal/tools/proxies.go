package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"

	"github.com/google/jsonschema-go/jsonschema"

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

// Tool definitions prepare typed contracts; registration is at the end of this file.
func defineListProxies(api ProxyAPI) ToolDefinition {
	proxyListSchema, err := jsonschema.For[listProxiesOutput](nil)
	if err != nil {
		panic(err)
	}
	return defineTool(&mcp.Tool{
		Meta:         mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": client.ListProxiesPath}},
		Name:         "list_proxies",
		OutputSchema: proxyListSchema,
		Description:  "List all proxy hosts visible to the configured HyperHive account, including domains, forwarding targets, TLS settings, enabled state, metadata and locations. Calls GET /proxy/list. Takes no arguments.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listProxiesInput) (*mcp.CallToolResult, any, error) {
		proxies, err := api.ListProxies(ctx)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(listProxiesOutput{Proxies: proxies})
		if err != nil {
			return nil, nil, errors.New("could not encode proxy list")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}

// RegisterProxies is the complete tool and safety catalog for this area.
// Keep names and explicit levels here; definitions contain handlers and schemas.
func RegisterProxies(server *Registry, api ProxyAPI) {
	server = server.Area("proxies")
	RegisterTool(server, "create_proxy", defineCreateProxy(api), safety.High)
	RegisterTool(server, "disable_proxy", defineDisableProxy(api), safety.High)
	RegisterTool(server, "enable_proxy", defineEnableProxy(api), safety.High)
	RegisterTool(server, "list_proxies", defineListProxies(api), safety.Low)
	RegisterTool(server, "setup_frontend_proxy", defineSetupFrontendProxy(api), safety.High)
	RegisterTool(server, "setup_secure_frontend_proxy", defineSetupSecureFrontendProxy(api), safety.High)
	RegisterTool(server, "update_proxy", defineUpdateProxy(api), safety.High)
}
