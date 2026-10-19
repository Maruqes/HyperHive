package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NginxAPI contains only explicitly reviewed NGINX operations, never generic dispatch.
type NginxAPI interface {
	DisableHost404(context.Context, client.NginxIDInput) error
	EnableHost404(context.Context, client.NginxIDInput) error
	UpdateHost404(context.Context, client.UpdateHost404Config) error
	CreateHost404(context.Context, client.Host404Config) error
	ListHosts404(context.Context) ([]map[string]any, error)
	DisableRedirect(context.Context, client.NginxIDInput) error
	EnableRedirect(context.Context, client.NginxIDInput) error
	UpdateRedirect(context.Context, client.UpdateRedirectConfig) error
	CreateRedirect(context.Context, client.RedirectConfig) error
	ListRedirects(context.Context) ([]map[string]any, error)
	DisableStream(context.Context, client.NginxIDInput) error
	EnableStream(context.Context, client.NginxIDInput) error
	UpdateStream(context.Context, client.UpdateStreamConfig) error
	CreateStream(context.Context, client.StreamConfig) error
	ListStreams(context.Context) ([]map[string]any, error)
	CreateAccessList(context.Context, client.AccessListConfig) error
	ListAccessLists(context.Context) ([]map[string]any, error)
	RenewCertificate(context.Context, client.NginxIDInput) error
	DownloadCertificate(context.Context, client.NginxIDInput) (client.CertificateDownload, error)
	ListDNSProviders(context.Context) ([]map[string]any, error)
	ListCertificates(context.Context) ([]map[string]any, error)
}

func registerNginx(server *mcp.Server, api NginxAPI) {
	registerDisableHost404(server, api)
	registerEnableHost404(server, api)
	registerUpdateHost404(server, api)
	registerCreateHost404(server, api)
	registerListHosts404(server, api)
	registerDisableRedirect(server, api)
	registerEnableRedirect(server, api)
	registerUpdateRedirect(server, api)
	registerCreateRedirect(server, api)
	registerListRedirects(server, api)
	registerDisableStream(server, api)
	registerEnableStream(server, api)
	registerUpdateStream(server, api)
	registerCreateStream(server, api)
	registerListStreams(server, api)
	registerCreateAccessList(server, api)
	registerListAccessLists(server, api)
	registerRenewCertificate(server, api)
	registerDownloadCertificate(server, api)
	registerListDNSProviders(server, api)
	registerListCertificates(server, api)
}

type nginxListOutput struct {
	Items []map[string]any `json:"items" jsonschema:"Complete API objects, preserving all fields; null root list is normalized to an empty array"`
}

func nginxTool(name, method, path, description string, readOnly bool) *mcp.Tool {
	destructive := !readOnly
	return &mcp.Tool{Name: name, Description: description + " Calls " + method + " " + path + ".", Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: readOnly}}
}

// Explicit raw output avoids the SDK's object-default float conversion, so
// arbitrary API metadata integers survive in both structured JSON and text.
func registerNginxList(server *mcp.Server, tool *mcp.Tool, call func(context.Context) ([]map[string]any, error)) {
	schema, err := jsonschema.For[nginxListOutput](nil)
	if err != nil {
		panic(err)
	}
	tool.OutputSchema = schema
	addExactTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		items, err := call(ctx)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(nginxListOutput{items})
		if err != nil {
			return nil, nil, errors.New("could not encode NGINX list")
		}
		return &mcp.CallToolResult{StructuredContent: json.RawMessage(data), Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
	})
}

func registerNginxMutation[In any](server *mcp.Server, tool *mcp.Tool, call func(context.Context, In) error) {
	// Explicitly constrain the API's string-or-number redirect status in discovery.
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	if _, ok := schema.Properties["forward_http_code"]; ok {
		schema.Properties["forward_http_code"] = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Pattern: "^[0-9]+$"}, {Type: "integer"}}}
	}
	tool.InputSchema = schema
	addExactTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, mutationOutput, error) {
		if err := validateNginxInput(req, input); err != nil {
			return nil, mutationOutput{}, err
		}
		if err := call(ctx, input); err != nil {
			return nil, mutationOutput{}, err
		}
		return nil, mutationOutput{Success: true}, nil
	})
}

func validateNginxInput(req *mcp.CallToolRequest, input any) error {
	var raw any
	if json.Unmarshal(req.Params.Arguments, &raw) != nil {
		return errors.New("invalid NGINX arguments")
	}
	var check func(any) bool
	check = func(v any) bool {
		if v == nil {
			return false
		}
		switch x := v.(type) {
		case map[string]any:
			for _, v := range x {
				if !check(v) {
					return false
				}
			}
		case []any:
			for _, v := range x {
				if !check(v) {
					return false
				}
			}
		}
		return true
	}
	if !check(raw) {
		return errors.New("NGINX arguments must not contain null")
	}
	switch in := input.(type) {
	case client.NginxIDInput:
		return positiveNginxID(in.ID)
	case client.StreamConfig:
		return validateStream(in)
	case client.UpdateStreamConfig:
		if err := positiveNginxID(in.ID); err != nil {
			return err
		}
		return validateStream(in.StreamConfig)
	case client.RedirectConfig:
		return validateRedirect(in)
	case client.UpdateRedirectConfig:
		if err := positiveNginxID(in.ID); err != nil {
			return err
		}
		return validateRedirect(in.RedirectConfig)
	case client.Host404Config:
		return validateHost404(in)
	case client.UpdateHost404Config:
		if err := positiveNginxID(in.ID); err != nil {
			return err
		}
		return validateHost404(in.Host404Config)
	case client.AccessListConfig:
		if strings.TrimSpace(in.Name) == "" || in.Items == nil || in.Clients == nil {
			return errors.New("name must be nonempty and items/clients must be arrays")
		}
		for _, item := range in.Items {
			if strings.TrimSpace(item.Username) == "" {
				return errors.New("username must be nonempty")
			}
		}
		for _, rule := range in.Clients {
			if (rule.Directive != "allow" && rule.Directive != "deny") || strings.TrimSpace(rule.Address) == "" {
				return errors.New("client rules require allow/deny and a nonempty address")
			}
		}
	}
	return nil
}
func positiveNginxID(id int) error {
	if id < 1 {
		return errors.New("id must be positive")
	}
	return nil
}
func validateStream(in client.StreamConfig) error {
	if in.IncomingPort < 1 || in.IncomingPort > 65535 || in.ForwardingPort < 1 || in.ForwardingPort > 65535 || strings.TrimSpace(in.ForwardingHost) == "" || in.CertificateID < 0 {
		return errors.New("stream requires ports in 1–65535, a forwarding host and a nonnegative certificate_id")
	}
	return nil
}
func validateNginxDomains(domains []string, cert int) error {
	if len(domains) == 0 || cert < 0 {
		return errors.New("domain_names must be a nonempty array and certificate_id must be nonnegative")
	}
	for _, domain := range domains {
		if strings.TrimSpace(domain) == "" {
			return errors.New("domain_names must not contain empty domains")
		}
	}
	return nil
}
func validateHost404(in client.Host404Config) error {
	return validateNginxDomains(in.DomainNames, in.CertificateID)
}
func validateRedirect(in client.RedirectConfig) error {
	if err := validateNginxDomains(in.DomainNames, in.CertificateID); err != nil {
		return err
	}
	if (in.ForwardScheme != "http" && in.ForwardScheme != "https" && in.ForwardScheme != "$scheme") || strings.TrimSpace(in.ForwardDomainName) == "" {
		return errors.New("redirect requires http, https or $scheme and a nonempty forward_domain_name")
	}
	var code int
	switch value := in.ForwardHTTPCode.(type) {
	case string:
		var err error
		code, err = strconv.Atoi(value)
		if err != nil {
			return errors.New("forward_http_code must be an integer or decimal string")
		}
	case json.Number:
		value64, err := value.Int64()
		if err != nil || value64 < 300 || value64 > 399 {
			return errors.New("forward_http_code must be an integer between 300 and 399")
		}
		code = int(value64)
	case float64:
		if math.Trunc(value) != value {
			return errors.New("forward_http_code must be an integer")
		}
		code = int(value)
	default:
		return errors.New("forward_http_code must be an integer or decimal string")
	}
	if code < 300 || code > 399 {
		return fmt.Errorf("forward_http_code must be between 300 and 399")
	}
	return nil
}

func registerListCertificates(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_certificates", "GET", "/certs/list", "List certificate metadata visible to the configured account, preserving the complete HyperHive response objects. Takes no arguments.", true), api.ListCertificates)
}

func registerListDNSProviders(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_dns_providers", "GET", "/certs/dns-providers", "List supported DNS providers, names and credential-format templates. Templates are not configured credentials. Takes no arguments.", true), api.ListDNSProviders)
}

func registerDownloadCertificate(server *mcp.Server, api NginxAPI) {
	addExactTool(server, nginxTool("download_certificate", "POST", "/certs/download", "Download a certificate archive. The returned base64 data can contain private keys: treat it as secret. Preserves content type and disposition; never writes files. Maximum API response 16 MiB. Timeout three minutes, honoring cancellation. Read-only despite POST.", true), func(ctx context.Context, req *mcp.CallToolRequest, input client.NginxIDInput) (*mcp.CallToolResult, client.CertificateDownload, error) {
		if err := validateNginxInput(req, input); err != nil {
			return nil, client.CertificateDownload{}, err
		}
		result, err := api.DownloadCertificate(ctx, input)
		return nil, result, err
	})
}

func registerRenewCertificate(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("renew_certificate", "POST", "/certs/renew", "Renew a Let's Encrypt certificate. Only the API decides provider eligibility. Waits for the API response, with an eleven-minute timeout covering its ten-minute downstream operation. Cancellation or timeout can leave work running in HyperHive: inspect certificate state before retrying. Returns acceptance only, not certificate data. No automatic retry.", false), api.RenewCertificate)
}

func registerListAccessLists(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_access_lists", "GET", "/access-lists/list", "List expanded access lists, including owner, authentication items and network clients. Preserve API masking and all returned fields. Takes no arguments.", true), api.ListAccessLists)
}

func registerCreateAccessList(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("create_access_list", "POST", "/access-lists/create", "Create an access list with explicit name, satisfy_any, pass_auth, authentication items and client rules. Empty arrays are supported. Passwords are sensitive; the API may mask them on listing. No existing entries are edited. Returns acceptance only; no created ID is returned and no automatic retry occurs.", false), api.CreateAccessList)
}

func registerListStreams(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_streams", "GET", "/stream/list", "List streams, preserving every field returned by HyperHive. Takes no arguments.", true), api.ListStreams)
}

func registerCreateStream(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("create_stream", "POST", "/stream/create", "Create a stream. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Stream changes persist NPM first and the description second; a description failure can leave NPM changed. Empty description explicitly overwrites existing text. Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.CreateStream)
}

func registerUpdateStream(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("update_stream", "PUT", "/stream/edit", "Update a stream. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Stream changes persist NPM first and the description second; a description failure can leave NPM changed. Empty description explicitly overwrites existing text. Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.UpdateStream)
}

func registerEnableStream(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("enable_stream", "POST", "/stream/enable", "Enable a stream.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.EnableStream)
}

func registerDisableStream(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("disable_stream", "POST", "/stream/disable", "Disable a stream.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.DisableStream)
}

func registerListRedirects(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_redirects", "GET", "/redirection/list", "List redirects, preserving every field returned by HyperHive. Takes no arguments.", true), api.ListRedirects)
}

func registerCreateRedirect(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("create_redirect", "POST", "/redirection/create", "Create a redirect. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.CreateRedirect)
}

func registerUpdateRedirect(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("update_redirect", "PUT", "/redirection/edit", "Update a redirect. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.UpdateRedirect)
}

func registerEnableRedirect(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("enable_redirect", "POST", "/redirection/enable", "Enable a redirect.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.EnableRedirect)
}

func registerDisableRedirect(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("disable_redirect", "POST", "/redirection/disable", "Disable a redirect.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.DisableRedirect)
}

func registerListHosts404(server *mcp.Server, api NginxAPI) {
	registerNginxList(server, nginxTool("list_404_hosts", "GET", "/404/list", "List 404 hosts, preserving every field returned by HyperHive. Takes no arguments.", true), api.ListHosts404)
}

func registerCreateHost404(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("create_404_host", "POST", "/404/create", "Create a 404 host. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.CreateHost404)
}

func registerUpdateHost404(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("update_404_host", "PUT", "/404/edit", "Update a 404 host. Supply complete writable configuration, including explicit false/zero settings. Enabled state uses dedicated actions; server-owned IDs are not create fields.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.UpdateHost404)
}

func registerEnableHost404(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("enable_404_host", "POST", "/404/enable", "Enable a 404 host.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.EnableHost404)
}

func registerDisableHost404(server *mcp.Server, api NginxAPI) {
	registerNginxMutation(server, nginxTool("disable_404_host", "POST", "/404/disable", "Disable a 404 host.  Returns acceptance only; never fabricates IDs/state and never automatically retries.", false), api.DisableHost404)
}
