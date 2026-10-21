package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// addExactTool retains SDK schema validation and output handling, but decodes
// the original arguments again before invoking application code. The SDK's
// schema/default processing passes numbers through float64, which can round
// int64 resource IDs and silently select a different mutation target. Tool
// schemas in this registry do not use defaults.
func addExactTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) error {
	if tool.InputSchema == nil {
		schema, err := jsonschema.For[In](&jsonschema.ForOptions{})
		if err != nil {
			return fmt.Errorf("derive tool input schema: %w", err)
		}
		tool.InputSchema = schema
	}
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, Out, error) {
		var input In
		var output Out
		if req == nil || req.Params == nil {
			return nil, output, errors.New("invalid tool arguments")
		}
		raw := req.Params.Arguments
		if len(raw) != 0 {
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(raw) {
				return nil, output, errors.New("tool arguments must be an object with valid field types")
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&input); err != nil {
				return nil, output, errors.New("tool arguments must be an object with valid field types")
			}
		}
		return handler(ctx, req, input)
	})
	return nil
}
