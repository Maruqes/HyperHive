package client

import "context"

// Endpoint paths are shared with executable tool metadata for coverage review.
const (
	ListProxiesPath              = "/proxy/list"
	CreateProxyPath              = "/proxy/create"
	UpdateProxyPath              = "/proxy/edit"
	EnableProxyPath              = "/proxy/enable"
	DisableProxyPath             = "/proxy/disable"
	SetupFrontendProxyPath       = "/proxy/setupFrontEnd"
	SetupSecureFrontendProxyPath = "/proxy/setupFrontEndSecure"
)

// ProxyConfig is the complete writable configuration accepted by HyperHive's
// create/edit helpers. Enabled is changed through the dedicated action tools.
// AccessListID accepts a nonnegative integer or its decimal string form.
type ProxyConfig struct {
	DomainNames           []string       `json:"domain_names"`
	ForwardScheme         string         `json:"forward_scheme"`
	ForwardHost           string         `json:"forward_host"`
	ForwardPort           int            `json:"forward_port"`
	CachingEnabled        bool           `json:"caching_enabled"`
	BlockExploits         bool           `json:"block_exploits"`
	AllowWebsocketUpgrade bool           `json:"allow_websocket_upgrade"`
	AccessListID          any            `json:"access_list_id" jsonschema:"Nonnegative access-list ID as an integer or decimal string; zero means no access list"`
	CertificateID         int            `json:"certificate_id"`
	Meta                  map[string]any `json:"meta"`
	AdvancedConfig        string         `json:"advanced_config"`
	Locations             []Location     `json:"locations"`
	HTTP2Support          bool           `json:"http2_support"`
	HSTSEnabled           bool           `json:"hsts_enabled"`
	HSTSSubdomains        bool           `json:"hsts_subdomains"`
	SSLForced             bool           `json:"ssl_forced"`
}

type Location struct {
	Path          string `json:"path"`
	ForwardScheme string `json:"forward_scheme"`
	ForwardHost   string `json:"forward_host"`
	ForwardPort   int    `json:"forward_port"`
}

func (c *Client) CreateProxy(ctx context.Context, input ProxyConfig) error {
	return c.request(ctx, "POST", CreateProxyPath, input, nil)
}

// UpdateProxyInput requires a complete configuration rather than patch semantics.
type UpdateProxyInput struct {
	ProxyConfig
	ID int `json:"id"`
}

func (c *Client) UpdateProxy(ctx context.Context, input UpdateProxyInput) error {
	return c.request(ctx, "PUT", UpdateProxyPath, input, nil)
}

type ProxyIDInput struct {
	ID int `json:"id"`
}

func (c *Client) EnableProxy(ctx context.Context, input ProxyIDInput) error {
	return c.request(ctx, "POST", EnableProxyPath, input, nil)
}

func (c *Client) DisableProxy(ctx context.Context, input ProxyIDInput) error {
	return c.request(ctx, "POST", DisableProxyPath, input, nil)
}

// FrontendProxyInput.ID is a certificate ID, not a proxy ID.
type FrontendProxyInput struct {
	Domain string `json:"domain"`
	ID     int    `json:"id" jsonschema:"Certificate ID; zero means no TLS certificate"`
}

func (c *Client) SetupFrontendProxy(ctx context.Context, input FrontendProxyInput) error {
	return c.request(ctx, "POST", SetupFrontendProxyPath, input, nil)
}

type SecureFrontendProxyInput struct {
	PublicDomain         string `json:"publicDomain"`
	PublicCertificateID  int    `json:"publicCertificateId"`
	PrivateDomain        string `json:"privateDomain"`
	PrivateCertificateID int    `json:"privateCertificateId"`
}

func (c *Client) SetupSecureFrontendProxy(ctx context.Context, input SecureFrontendProxyInput) error {
	return c.request(ctx, "POST", SetupSecureFrontendProxyPath, input, nil)
}
