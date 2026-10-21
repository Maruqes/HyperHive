package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SPAAPI exposes only reviewed SPA read operations.
type SPAAPI interface {
	ListSPAAllows(context.Context, client.SPAPortInput) (client.SPAObject, error)
	ListSPAPorts(context.Context) (client.SPAObject, error)
}

type spaOutput struct {
	Result client.SPAObject `json:"result" jsonschema:"Complete safe SPA API response; passwords and hashes are never returned"`
}

func defineListSpaAllows(api SPAAPI) ToolDefinition {
	return addSPATool("list_spa_allows", "/spa/allow/{port}", "List IP permissions and their remaining seconds for a configured SPA port. Zero remaining_seconds is preserved. Requires management authentication, no password. Calls GET /spa/allow/{port}.", func(ctx context.Context, input client.SPAPortInput) (client.SPAObject, error) {
		if err := client.ValidateSPAPort(input); err != nil {
			return nil, err
		}
		return api.ListSPAAllows(ctx, input)
	})
}

func defineListSpaPorts(api SPAAPI) ToolDefinition {
	return addSPATool("list_spa_ports", client.SPAPortsPath, "List SPA-protected ports, descriptions and creation timestamps. Returns spa_ports, including an empty array when no ports exist. Does not retrieve passwords or password hashes. Calls GET /spa/.", func(ctx context.Context, _ struct{}) (client.SPAObject, error) { return api.ListSPAPorts(ctx) })
}

func addSPATool[In any](name string, path string, description string, call func(context.Context, In) (client.SPAObject, error)) ToolDefinition {
	destructive := false
	schema, err := jsonschema.For[spaOutput](nil)
	if err != nil {
		panic(err)
	}
	tool := &mcp.Tool{Name: name, Description: description, OutputSchema: schema, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "GET", "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &destructive, IdempotentHint: true}}
	return defineTool(tool, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		result, err := call(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		if result == nil {
			return nil, nil, errors.New("HyperHive API returned an invalid SPA result")
		}
		data, err := json.Marshal(spaOutput{Result: result})
		if err != nil {
			return nil, nil, errors.New("could not encode SPA result")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}

// RegisterSPA is the complete tool and safety catalog for this area.
// Keep names and explicit levels here; definitions contain handlers and schemas.
func RegisterSPA(server *Registry, api SPAAPI) {
	server = server.Area("spa")
	RegisterTool(server, "list_spa_allows", defineListSpaAllows(api), safety.Low)
	RegisterTool(server, "list_spa_ports", defineListSpaPorts(api), safety.Low)
}
