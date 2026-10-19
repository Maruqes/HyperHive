package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ISOsAPI contains only reviewed non-deletion ISO operations.
type ISOsAPI interface {
	ListISOs(context.Context) ([]client.ISOObject, error)
	StartISODownload(context.Context, client.StartISODownloadInput) (client.ISOAcceptance, error)
}

type isosListOutput struct {
	Result []client.ISOObject `json:"result"`
}

type isosAcceptanceOutput struct {
	Result client.ISOAcceptance `json:"result"`
}

// RegisterISOs exposes the permitted ISO API contracts across both transports.
func RegisterISOs(server *mcp.Server, api ISOsAPI) {
	addISOsTool(server, "start_iso_download", "POST", client.ISOsPath+"download", "Start an asynchronous ISO download on an existing NFS share on a connected slave. Calls POST /isos/download. Requires an absolute http(s) url without credentials or fragment, a leaf iso_name without path separators or NUL, both ending exactly with .iso, and a positive nfs_share_id. HTTP 202 status started means accepted only, never downloaded or registered: download, sync or registration can fail later and report notifications. There is no job ID or completion endpoint. Partial files can remain after failure. No automatic retries.", func(ctx context.Context, input client.StartISODownloadInput) (isosAcceptanceOutput, error) {
		if err := client.ValidateStartISODownload(input); err != nil {
			return isosAcceptanceOutput{}, err
		}
		result, err := api.StartISODownload(ctx, input)
		return isosAcceptanceOutput{Result: result}, err
	})
	addISOsTool(server, "list_isos", "GET", client.ISOsPath, "List registered ISO images and best-effort availability on connected slaves. Calls GET /isos/. False availability can mean a missing file or a failed probe; this is not a download completion guarantee. Takes no arguments.", func(ctx context.Context, _ struct{}) (isosListOutput, error) {
		result, err := api.ListISOs(ctx)
		return isosListOutput{Result: result}, err
	})
}

func addISOsTool[In, Out any](server *mcp.Server, name, method, path, description string, call func(context.Context, In) (Out, error)) {
	destructive := false
	schema, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(err)
	}
	tool := &mcp.Tool{Name: name, Description: description, OutputSchema: schema, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: method == "GET"}}
	addExactTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		var args map[string]json.RawMessage
		if len(req.Params.Arguments) != 0 && (json.Unmarshal(req.Params.Arguments, &args) != nil || args == nil) {
			return nil, nil, errors.New("invalid ISO arguments")
		}
		for _, value := range args {
			if string(value) == "null" {
				return nil, nil, errors.New("ISO arguments must not contain null")
			}
		}
		out, err := call(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(out)
		if err != nil {
			return nil, nil, errors.New("could not encode ISO result")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}
