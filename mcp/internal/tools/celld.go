package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

// CelldAPI is deliberately limited to the reviewed, named Celld operations.
type CelldAPI interface {
	GetCelldEvents(context.Context, client.CelldObservationInput) (client.CelldObject, error)
	GetCelldLogs(context.Context, client.CelldObservationInput) (client.CelldObject, error)
	GetCelldCredentials(context.Context, client.CelldFleetIDInput) (client.CelldObject, error)
	StopCelldFleet(context.Context, client.CelldFleetIDInput) (client.CelldObject, error)
	StartCelldFleet(context.Context, client.CelldFleetIDInput) (client.CelldObject, error)
	CreateCelldFleet(context.Context, client.CreateCelldFleetInput) (client.CelldObject, error)
	GetCelldHealth(context.Context) (client.CelldObject, error)
	ListCelldFleets(context.Context) (client.CelldObject, error)
}

type celldOutput struct {
	Result client.CelldObject `json:"result" jsonschema:"Complete HyperHive API response, preserving all fields including partial-state diagnostics"`
}

func celldTool(name, method, path, description string, destructive bool) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: method == "GET"}}
}

func defineListCelldFleets(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("list_celld_fleets", "GET", client.CelldFleetsPath, "List Celld fleets and shared storage status. Calls GET /celld/fleets. Preserves reset_pending, storage errors and partial-state diagnostics. Takes no arguments.", false), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, celldOutput, error) {
		result, err := api.ListCelldFleets(ctx)
		return nil, celldOutput{result}, err
	})
}

func defineGetCelldHealth(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("get_celld_health", "GET", client.CelldHealthPath, "Get Celld storage, fleets and machines health. Calls GET /celld/health. Unhealthy HTTP 503 diagnostics are returned as a normal result with healthy=false, not discarded. Takes no arguments.", false), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, celldOutput, error) {
		result, err := api.GetCelldHealth(ctx)
		return nil, celldOutput{result}, err
	})
}

// Supplied null is not omission. Reject it before marshaling pointer/slice inputs.
func rejectCelldNulls(req *mcp.CallToolRequest) error {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errors.New("invalid Celld arguments")
	}
	var check func(any) bool
	check = func(v any) bool {
		if v == nil {
			return false
		}
		switch value := v.(type) {
		case map[string]any:
			for _, child := range value {
				if !check(child) {
					return false
				}
			}
		case []any:
			for _, child := range value {
				if !check(child) {
					return false
				}
			}
		}
		return true
	}
	if !check(args) {
		return errors.New("Celld arguments must not contain null; omit optional fields instead")
	}
	return nil
}

func defineCreateCelldFleet(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("create_celld_fleet", "POST", client.CelldFleetsPath, "Create a Celld fleet using inventory server names. Calls POST /celld/fleets. Omit ports for automatic allocation or supply distinct HTTP/internal ports. Returns the API's queued fleet state (202), not completed provisioning. Name uniqueness, server inventory, port reservations and pending legacy reset are checked by HyperHive. No automatic retry.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CreateCelldFleetInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if input.Name == "" || len(input.Name) > 128 || input.Name != strings.TrimSpace(input.Name) {
			return nil, celldOutput{}, errors.New("name must have 1–128 bytes without surrounding whitespace")
		}
		if len(input.Servers) == 0 {
			return nil, celldOutput{}, errors.New("servers must contain at least one server")
		}
		seen := map[string]bool{}
		for _, name := range input.Servers {
			if name == "" || seen[name] {
				return nil, celldOutput{}, errors.New("servers must contain nonempty unique names")
			}
			seen[name] = true
		}
		if p := input.Ports; p != nil && (p.HTTP < 1 || p.HTTP > 65535 || p.Internal < 1 || p.Internal > 65535 || p.HTTP == p.Internal) {
			return nil, celldOutput{}, errors.New("ports must be distinct integers between 1 and 65535")
		}
		result, err := api.CreateCelldFleet(ctx, input)
		return nil, celldOutput{result}, err
	})
}

func defineStartCelldFleet(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("start_celld_fleet", "POST", client.CelldFleetsPath+"/{id}/start", "Start a Celld fleet. Returns queued desired_state=running (HTTP 202), not proof that nodes are ready. Busy or conflicting operations are reported by HyperHive. No automatic retry.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CelldFleetIDInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if err := client.ValidateCelldFleetID(input.ID); err != nil {
			return nil, celldOutput{}, err
		}
		result, err := api.StartCelldFleet(ctx, input)
		return nil, celldOutput{result}, err
	})
}

func defineStopCelldFleet(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("stop_celld_fleet", "POST", client.CelldFleetsPath+"/{id}/stop", "Stop a Celld fleet while retaining its configuration and data. Returns queued desired_state=stopped (HTTP 202), not completed shutdown. Busy/conflicting operations are reported by HyperHive. No automatic retry.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CelldFleetIDInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if err := client.ValidateCelldFleetID(input.ID); err != nil {
			return nil, celldOutput{}, err
		}
		result, err := api.StopCelldFleet(ctx, input)
		return nil, celldOutput{result}, err
	})
}

func defineGetCelldCredentials(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("get_celld_credentials", "GET", client.CelldFleetsPath+"/{id}/credentials", "Explicitly retrieve sensitive fleet-scoped AWS storage credentials. Calls GET /celld/fleets/{id}/credentials. Returns aws_access_key_id and aws_secret_access_key; treat the result as secret. Deleting fleets may reject this request with HTTP 409. Other tools do not fetch these credentials.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CelldFleetIDInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if err := client.ValidateCelldFleetID(input.ID); err != nil {
			return nil, celldOutput{}, err
		}
		result, err := api.GetCelldCredentials(ctx, input)
		return nil, celldOutput{result}, err
	})
}

func defineGetCelldLogs(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("get_celld_logs", "GET", client.CelldFleetsPath+"/{id}/logs", "Read Celld logs with time, node, workload, source, severity and text filters and opaque cursor pagination. Calls GET /celld/fleets/{id}/logs. Preserves incomplete, error, truncated and next_cursor. Omit times for the API's last-hour default within seven-day retention. download=true returns parsed NDJSON records and header-derived truncated/next_cursor; export does not provide from/to or completeness metadata. Not a live stream.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CelldObservationInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if err := client.ValidateCelldObservationInput(input); err != nil {
			return nil, celldOutput{}, err
		}
		result, err := api.GetCelldLogs(ctx, input)
		return nil, celldOutput{result}, err
	})
}

// Keep typed SDK input validation while preserving large observation integers.
// SDK v1.8.0's automatic object-output defaults round-trip numbers via float64.
// Supply the already validated object as raw JSON to both MCP result forms.
func addCelldTool[In any](tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, celldOutput, error)) ToolDefinition {
	schema, err := jsonschema.For[celldOutput](nil)
	if err != nil {
		panic(err)
	}
	tool.OutputSchema = schema
	return defineTool(tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		_, out, err := handler(ctx, req, input)
		if err != nil {
			return nil, nil, err
		}
		if out.Result == nil {
			return nil, nil, errors.New("HyperHive API returned an invalid Celld result")
		}
		data, err := json.Marshal(out)
		if err != nil {
			return nil, nil, errors.New("could not encode Celld result")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}

func defineGetCelldEvents(api CelldAPI) ToolDefinition {
	return addCelldTool(celldTool("get_celld_events", "GET", client.CelldFleetsPath+"/{id}/events", "Read Celld events with time, node, workload, source, severity and text filters and opaque cursor pagination. Calls GET /celld/fleets/{id}/events. Preserves incomplete, error, truncated and next_cursor. Omit times for the API's last-hour default within seven-day retention. download=true returns parsed NDJSON records and header-derived truncated/next_cursor; export does not provide from/to or completeness metadata. Not a live stream.", false), func(ctx context.Context, req *mcp.CallToolRequest, input client.CelldObservationInput) (*mcp.CallToolResult, celldOutput, error) {
		if err := rejectCelldNulls(req); err != nil {
			return nil, celldOutput{}, err
		}
		if err := client.ValidateCelldObservationInput(input); err != nil {
			return nil, celldOutput{}, err
		}
		result, err := api.GetCelldEvents(ctx, input)
		return nil, celldOutput{result}, err
	})
}

// RegisterCelld is the complete tool and safety catalog for this area.
// Keep names and explicit levels here; definitions contain handlers and schemas.
func RegisterCelld(server *Registry, api CelldAPI) {
	server = server.Area("celld")
	RegisterTool(server, "create_celld_fleet", defineCreateCelldFleet(api), safety.High)
	RegisterTool(server, "get_celld_credentials", defineGetCelldCredentials(api), safety.High)
	RegisterTool(server, "get_celld_events", defineGetCelldEvents(api), safety.Low)
	RegisterTool(server, "get_celld_health", defineGetCelldHealth(api), safety.Low)
	RegisterTool(server, "get_celld_logs", defineGetCelldLogs(api), safety.Low)
	RegisterTool(server, "list_celld_fleets", defineListCelldFleets(api), safety.Low)
	RegisterTool(server, "start_celld_fleet", defineStartCelldFleet(api), safety.High)
	RegisterTool(server, "stop_celld_fleet", defineStopCelldFleet(api), safety.High)
}
