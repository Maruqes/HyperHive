package tools

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mutationOutput struct {
	Success bool `json:"success" jsonschema:"The HyperHive API accepted the operation; no resource ID or resulting state is returned"`
}

func validateTarget(scheme, host string, port int) error {
	if scheme != "http" && scheme != "https" {
		return errors.New("forward_scheme must be http or https")
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("forward_host must not be empty")
	}
	if port < 1 || port > 65535 {
		return errors.New("forward_port must be between 1 and 65535")
	}
	return nil
}

func validateConfig(input client.ProxyConfig) error { return validateConfigWithPreserve(input, false) }
func validateConfigWithPreserve(input client.ProxyConfig, preserve bool) error {
	if len(input.DomainNames) == 0 {
		return errors.New("domain_names must contain at least one domain")
	}
	for _, domain := range input.DomainNames {
		if strings.TrimSpace(domain) == "" {
			return errors.New("domain_names must not contain empty domains")
		}
	}
	if err := validatePoolTarget(input.UpstreamID, input.ForwardScheme, input.ForwardHost, input.ForwardPort, preserve); err != nil {
		return err
	}
	if input.CertificateID < 0 {
		return errors.New("certificate_id must be nonnegative")
	}
	switch id := input.AccessListID.(type) {
	case string:
		if _, err := strconv.ParseUint(id, 10, 63); err != nil {
			return errors.New("access_list_id must be a nonnegative integer or decimal string")
		}
	case json.Number:
		value, err := id.Int64()
		if err != nil || value < 0 || value > 9007199254740991 {
			return errors.New("access_list_id must be a nonnegative safe integer")
		}
	case float64:
		if id < 0 || id > 9007199254740991 || math.Trunc(id) != id {
			return errors.New("access_list_id must be a nonnegative safe integer")
		}
	default:
		return errors.New("access_list_id must be a nonnegative integer or decimal string")
	}
	if input.Locations == nil {
		return errors.New("locations must be an array; use [] for no locations")
	}
	if input.Meta == nil {
		return errors.New("meta must be an object; use {} for no metadata")
	}
	for _, loc := range input.Locations {
		if !strings.HasPrefix(loc.Path, "/") {
			return errors.New("location path must start with /")
		}
		if err := validatePoolTarget(loc.UpstreamID, loc.ForwardScheme, loc.ForwardHost, loc.ForwardPort, preserve); err != nil {
			return err
		}
	}
	return nil
}

func mutationTool(name, method, path, description string) *mcp.Tool {
	destructive := true
	return &mcp.Tool{Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Name: name, Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}
}

// Constrain the union field in discovery as well as in runtime validation.
func proxyConfigSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	zero := float64(0)
	maxID := float64(9007199254740991)
	schema.Properties["access_list_id"] = &jsonschema.Schema{Description: "Nonnegative access-list ID; zero means no access list", AnyOf: []*jsonschema.Schema{
		{Type: "string", Pattern: "^[0-9]+$"},
		{Type: "integer", Minimum: &zero, Maximum: &maxID},
	}}
	addUpstreamSelectionSchema(schema)
	return schema
}

func registerCreateProxy(server *mcp.Server, api ProxyAPI) {
	tool := mutationTool("create_proxy", "POST", client.CreateProxyPath, "Create a proxy host with complete configuration. Calls POST /proxy/create. Supply every setting, using false, zero, empty metadata/config and empty locations where appropriate. Enabled state is controlled by enable_proxy/disable_proxy; the API does not return the created ID. No automatic retry.")
	tool.InputSchema = proxyConfigSchema[client.ProxyConfig]()
	addExactTool(server, tool,
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.ProxyConfig) (*mcp.CallToolResult, mutationOutput, error) {
			if err := validateConfig(input); err != nil {
				return nil, mutationOutput{}, err
			}
			if err := api.CreateProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}

func registerUpdateProxy(server *mcp.Server, api ProxyAPI) {
	tool := mutationTool("update_proxy", "PUT", client.UpdateProxyPath, "Replace a proxy host's complete configuration. Calls PUT /proxy/edit. All settings and a positive id are required; omitted settings are rejected, false/zero settings are sent unchanged. Managed frontend proxies cannot be edited (403). The API checks the immutable resource ID for management restrictions. Enabled state uses separate actions. No automatic retry.")
	tool.InputSchema = proxyConfigSchema[client.UpdateProxyInput]()
	addExactTool(server, tool,
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.UpdateProxyInput) (*mcp.CallToolResult, mutationOutput, error) {
			if input.ID <= 0 {
				return nil, mutationOutput{}, errors.New("id must be positive")
			}
			if err := validateConfigWithPreserve(input.ProxyConfig, true); err != nil {
				return nil, mutationOutput{}, err
			}
			if err := api.UpdateProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}

func registerEnableProxy(server *mcp.Server, api ProxyAPI) {
	addExactTool(server, mutationTool("enable_proxy", "POST", client.EnableProxyPath, "Enable a proxy host by positive id. Calls POST /proxy/enable with JSON. Managed frontend proxies may be enabled. Returns acceptance only; no automatic retry."),
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.ProxyIDInput) (*mcp.CallToolResult, mutationOutput, error) {
			if input.ID <= 0 {
				return nil, mutationOutput{}, errors.New("id must be positive")
			}
			if err := api.EnableProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}

func registerDisableProxy(server *mcp.Server, api ProxyAPI) {
	addExactTool(server, mutationTool("disable_proxy", "POST", client.DisableProxyPath, "Disable a proxy host by positive id. Calls POST /proxy/disable with JSON. Managed frontend proxies cannot be disabled (403). Returns acceptance only; no automatic retry."),
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.ProxyIDInput) (*mcp.CallToolResult, mutationOutput, error) {
			if input.ID <= 0 {
				return nil, mutationOutput{}, errors.New("id must be positive")
			}
			if err := api.DisableProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}

func registerSetupFrontendProxy(server *mcp.Server, api ProxyAPI) {
	addExactTool(server, mutationTool("setup_frontend_proxy", "POST", client.SetupFrontendProxyPath, "Create or replace the managed frontend proxy matched by domain. Calls POST /proxy/setupFrontEnd. id is a certificate ID (zero means no TLS), not a proxy ID. The server configures frontend port 8079 and API_PORT locations /api, /novnc and /guest_api, with streaming/timeouts/IP-header settings. This replaces matching configuration. Returns acceptance only; no automatic retry."),
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.FrontendProxyInput) (*mcp.CallToolResult, mutationOutput, error) {
			if strings.TrimSpace(input.Domain) == "" {
				return nil, mutationOutput{}, errors.New("domain must not be empty")
			}
			if input.ID < 0 {
				return nil, mutationOutput{}, errors.New("id (certificate ID) must be nonnegative")
			}
			if err := api.SetupFrontendProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}

func registerSetupSecureFrontendProxy(server *mcp.Server, api ProxyAPI) {
	addExactTool(server, mutationTool("setup_secure_frontend_proxy", "POST", client.SetupSecureFrontendProxyPath, "Create or replace managed public/private frontend proxies. Calls POST /proxy/setupFrontEndSecure with publicDomain, publicCertificateId, privateDomain, privateCertificateId. Zero certificate ID means no TLS. Private exposes /api, /novnc and /guest_api; public exposes /guest_api and blocks root. Writes private then public, with NO rollback: an error can leave the private change applied. Inspect list_proxies before recovery. Returns acceptance only; no automatic retry."),
		func(ctx context.Context, _ *mcp.CallToolRequest, input client.SecureFrontendProxyInput) (*mcp.CallToolResult, mutationOutput, error) {
			if strings.TrimSpace(input.PublicDomain) == "" || strings.TrimSpace(input.PrivateDomain) == "" {
				return nil, mutationOutput{}, errors.New("publicDomain and privateDomain must not be empty")
			}
			if input.PublicCertificateID < 0 || input.PrivateCertificateID < 0 {
				return nil, mutationOutput{}, errors.New("certificate IDs must be nonnegative")
			}
			if err := api.SetupSecureFrontendProxy(ctx, input); err != nil {
				return nil, mutationOutput{}, err
			}
			return nil, mutationOutput{Success: true}, nil
		})
}
