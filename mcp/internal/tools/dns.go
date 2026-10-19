package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DNSAPI contains only reviewed, non-deleting DNS alias operations.
type DNSAPI interface {
	ListDNSAliases(context.Context) (client.DNSObject, error)
	GetDNSAlias(context.Context, client.DNSAliasInput) (client.DNSObject, error)
	AddDNSAlias(context.Context, client.DNSAliasWriteInput) error
}

// RegisterDNS exposes the same DNS tools to embedded HTTP and standalone stdio.
func RegisterDNS(server *mcp.Server, api DNSAPI) {
	tool := dnsTool("dns_aliases_list", "GET", "/dnsmasq/alias/all", "List all managed DNS aliases, including ssh_username and related_vm metadata. Existing records return empty strings from the updated backend. Preserve all API response fields.", true)
	tool.OutputSchema = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{"aliases": {Type: "array", Items: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"alias": {Type: "string"}, "ip": {Type: "string"},
		"ssh_username": {Type: "string", Description: "Manually entered SSH username; blank when unset"},
		"related_vm":   {Type: "string", Description: "Related VM name; blank when unlinked"},
	}, Required: []string{"alias", "ip"}}}}, Required: []string{"aliases"}}
	addExactTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		var raw map[string]json.RawMessage
		if len(req.Params.Arguments) > 0 && (json.Unmarshal(req.Params.Arguments, &raw) != nil || raw == nil) {
			return nil, nil, errors.New("DNS arguments must be an object")
		}
		out, err := api.ListDNSAliases(ctx)
		if err != nil {
			return nil, nil, err
		}
		return dnsResult(out)
	})
	get := dnsTool("dns_alias_get", "GET", "/dnsmasq/alias/get", "Check whether the exact DNS alias/address pair exists; false is a successful missing result.", true)
	get.OutputSchema = &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{"exists": {Type: "boolean"}}, Required: []string{"exists"}}
	addExactTool(server, get, func(ctx context.Context, req *mcp.CallToolRequest, in client.DNSAliasInput) (*mcp.CallToolResult, any, error) {
		if err := validateDNSInput(req, in); err != nil {
			return nil, nil, err
		}
		out, err := api.GetDNSAlias(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return dnsResult(out)
	})

	add := dnsTool("dns_alias_add", "POST", "/dnsmasq/alias/add", "Add a DNS alias or replace its IP and metadata, then restart DNS instances. Optional ssh_username is manual text; related_vm is an existing VM name from list_vms, validated by the backend. Omitted or empty metadata clears stored values, so include current values when preserving them. Alias and IP must contain no whitespace or control characters. Only an identical alias, IP and metadata is a no-op. Returns success:true, not the saved record; use dns_aliases_list to read metadata. Configuration may remain changed if writing includes or restarting fails; do not automatically retry.", false)
	addExactTool(server, add, func(ctx context.Context, req *mcp.CallToolRequest, in client.DNSAliasWriteInput) (*mcp.CallToolResult, mutationOutput, error) {
		if err := validateDNSInput(req, in); err != nil {
			return nil, mutationOutput{}, err
		}
		if err := api.AddDNSAlias(ctx, in); err != nil {
			return nil, mutationOutput{}, err
		}
		return nil, mutationOutput{Success: true}, nil
	})

}

func dnsResult(out client.DNSObject) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
}

func validateDNSInput(req *mcp.CallToolRequest, in interface{ Validate() error }) error {
	var raw map[string]json.RawMessage
	if json.Unmarshal(req.Params.Arguments, &raw) != nil || raw == nil {
		return errors.New("DNS arguments must be an object")
	}
	for _, value := range raw {
		if string(value) == "null" {
			return errors.New("DNS arguments must not contain null")
		}
	}
	return in.Validate()
}

func dnsTool(name, method, path, description string, readOnly bool) *mcp.Tool {
	destructive := !readOnly
	return &mcp.Tool{Name: name, Description: description + " Calls " + method + " " + path + ".", Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: readOnly}}
}
