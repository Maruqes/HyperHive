package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/jsonschema-go/jsonschema"
	"net/netip"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type UpstreamsAPI interface {
	ListUpstreams(context.Context) ([]map[string]any, error)
	GetUpstream(context.Context, client.UpstreamID) (map[string]any, error)
	GetUpstreamCapabilities(context.Context) (map[string]any, error)
	CreateUpstream(context.Context, client.UpstreamConfig) (map[string]any, error)
	UpdateUpstream(context.Context, client.UpstreamUpdate) (map[string]any, error)
}
type upstreamObjectOutput struct {
	Upstream map[string]any `json:"upstream"`
}
type upstreamListOutput struct {
	Upstreams []map[string]any `json:"upstreams"`
}
type upstreamCapabilitiesOutput struct {
	Capabilities map[string]any `json:"capabilities"`
}

func validateUpstreamConfig(in client.UpstreamConfig) error {
	if in.Name == "" || in.Context != "" && in.Context != "http" && in.Context != "stream" {
		return errors.New("name and valid http/stream context are required")
	}
	if len(in.Members) < 1 || len(in.Members) > 128 {
		return errors.New("server_arr requires 1 to 128 members")
	}
	for _, m := range in.Members {
		ip, err := netip.ParseAddr(m.IP)
		if err != nil || ip.Zone() != "" || m.Port < 1 || m.Port > 65535 {
			return errors.New("members require IP literals and ports 1 to 65535")
		}
	}
	if in.Context == "stream" && in.Method == "ip_hash" {
		return errors.New("ip_hash is HTTP-only")
	}
	// The server remains authoritative for build capabilities and settings grammar.
	return nil
}

func registerUpstreams(server *mcp.Server, api UpstreamsAPI) {
	addExactTool(server, upstreamOutputTool[upstreamListOutput](nginxTool("list_upstreams", "GET", "/upstream/list", "List reusable HTTP and stream pools with references, revisions and apply status. No arguments.", true)), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		ps, err := api.ListUpstreams(ctx)
		return upstreamResult(upstreamListOutput{Upstreams: ps}, err)
	})
	addExactTool(server, upstreamOutputTool[upstreamObjectOutput](nginxTool("get_upstream", "GET", "/upstream/{id}", "Read one upstream definition and its references by immutable ID.", true)), func(ctx context.Context, _ *mcp.CallToolRequest, in client.UpstreamID) (*mcp.CallToolResult, any, error) {
		if in.ID < 1 {
			return nil, upstreamObjectOutput{}, errors.New("id must be positive")
		}
		p, err := api.GetUpstream(ctx, in)
		return upstreamResult(upstreamObjectOutput{Upstream: p}, err)
	})
	addExactTool(server, upstreamOutputTool[upstreamCapabilitiesOutput](nginxTool("get_upstream_capabilities", "GET", "/upstream/capabilities", "Inspect exact-image method capabilities, context restrictions, hash keys and server parameters. Unknown status does not enable a method.", true)), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		p, err := api.GetUpstreamCapabilities(ctx)
		return upstreamResult(upstreamCapabilitiesOutput{Capabilities: p}, err)
	})
	addExactTool(server, upstreamOutputTool[upstreamObjectOutput](nginxTool("create_upstream", "POST", "/upstream/create", "Create and apply a reusable pool. settings_string accepts validated NGINX server parameters, never directives. HTTP is the default context. Returns the persisted definition only after activation. No automatic retry; inspect state after timeout.", false)), func(ctx context.Context, _ *mcp.CallToolRequest, in client.UpstreamConfig) (*mcp.CallToolResult, any, error) {
		if err := validateUpstreamConfig(in); err != nil {
			return nil, upstreamObjectOutput{}, err
		}
		p, err := api.CreateUpstream(ctx, in)
		return upstreamResult(upstreamObjectOutput{Upstream: p}, err)
	})
	addExactTool(server, upstreamOutputTool[upstreamObjectOutput](nginxTool("update_upstream", "PUT", "/upstream/{id}", "Replace a pool definition using its immutable ID and expected revision. Referencing consumers share the new membership. Stale revisions or external consumer drift return 409; failed apply returns 503 and may require recovery. No automatic retry.", false)), func(ctx context.Context, _ *mcp.CallToolRequest, in client.UpstreamUpdate) (*mcp.CallToolResult, any, error) {
		if in.ID < 1 || in.Revision < 1 {
			return nil, upstreamObjectOutput{}, errors.New("positive id and expected revision required")
		}
		if err := validateUpstreamConfig(in.UpstreamConfig); err != nil {
			return nil, upstreamObjectOutput{}, err
		}
		p, err := api.UpdateUpstream(ctx, in)
		return upstreamResult(upstreamObjectOutput{Upstream: p}, err)
	})
}

func upstreamOutputTool[T any](tool *mcp.Tool) *mcp.Tool {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	tool.OutputSchema = schema
	return tool
}
func upstreamResult(value any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}
