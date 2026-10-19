package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// NginxIDInput identifies an existing NGINX resource; IDs are never fabricated.
type NginxIDInput struct {
	ID int `json:"id" jsonschema:"Positive existing resource ID from the corresponding list tool"`
}
type NginxMeta struct {
	LetsEncryptAgree bool `json:"letsencrypt_agree"`
	DNSChallenge     bool `json:"dns_challenge"`
}
type StreamMeta struct {
	DNSProviderCredentials string `json:"dns_provider_credentials"`
	LetsEncryptAgree       bool   `json:"letsencrypt_agree"`
	DNSChallenge           bool   `json:"dns_challenge"`
}
type StreamConfig struct {
	IncomingPort   int        `json:"incoming_port"`
	ForwardingHost string     `json:"forwarding_host"`
	ForwardingPort int        `json:"forwarding_port"`
	TCPForwarding  bool       `json:"tcp_forwarding"`
	UDPForwarding  bool       `json:"udp_forwarding"`
	CertificateID  int        `json:"certificate_id"`
	Meta           StreamMeta `json:"meta"`
	Description    string     `json:"description" jsonschema:"Explicit description; empty string removes the description text; NPM changes can remain applied if saving this text fails"`
}
type UpdateStreamConfig struct {
	StreamConfig
	ID int `json:"id"`
}
type RedirectConfig struct {
	DomainNames       []string  `json:"domain_names"`
	ForwardScheme     string    `json:"forward_scheme"`
	ForwardDomainName string    `json:"forward_domain_name"`
	ForwardHTTPCode   any       `json:"forward_http_code" jsonschema:"HTTP redirect status as an integer or decimal string"`
	PreservePath      bool      `json:"preserve_path"`
	BlockExploits     bool      `json:"block_exploits"`
	CertificateID     int       `json:"certificate_id"`
	Meta              NginxMeta `json:"meta"`
	AdvancedConfig    string    `json:"advanced_config"`
	HTTP2Support      bool      `json:"http2_support"`
	HSTSEnabled       bool      `json:"hsts_enabled"`
	HSTSSubdomains    bool      `json:"hsts_subdomains"`
	SSLForced         bool      `json:"ssl_forced"`
}
type UpdateRedirectConfig struct {
	RedirectConfig
	ID int `json:"id"`
}
type Host404Config struct {
	DomainNames    []string  `json:"domain_names"`
	CertificateID  int       `json:"certificate_id"`
	Meta           NginxMeta `json:"meta"`
	AdvancedConfig string    `json:"advanced_config"`
	HTTP2Support   bool      `json:"http2_support"`
	HSTSEnabled    bool      `json:"hsts_enabled"`
	HSTSSubdomains bool      `json:"hsts_subdomains"`
	SSLForced      bool      `json:"ssl_forced"`
}
type UpdateHost404Config struct {
	Host404Config
	ID int `json:"id"`
}
type AccessListItem struct {
	Username string `json:"username"`
	Password string `json:"password" jsonschema:"Sensitive basic-auth password; forwarded only to the configured API"`
}
type AccessListClient struct {
	Directive string `json:"directive"`
	Address   string `json:"address"`
}
type AccessListConfig struct {
	Name       string             `json:"name"`
	SatisfyAny bool               `json:"satisfy_any"`
	PassAuth   bool               `json:"pass_auth"`
	Items      []AccessListItem   `json:"items"`
	Clients    []AccessListClient `json:"clients"`
}

func (c *Client) listNginxObjects(ctx context.Context, path string) ([]map[string]any, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	var objects []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&objects); err != nil {
		return nil, errors.New("HyperHive API returned an invalid NGINX list")
	}
	if objects == nil {
		objects = []map[string]any{}
	}
	for _, object := range objects {
		if object == nil {
			return nil, errors.New("HyperHive API returned an invalid NGINX list item")
		}
	}
	return objects, nil
}

// CertificateDownload can contain private keys. It is returned only by the
// explicit read-only download tool and is never logged or written to disk.
type CertificateDownload struct {
	DataBase64         string `json:"data_base64" jsonschema:"Base64-encoded archive bytes; may include private keys"`
	ContentType        string `json:"content_type"`
	ContentDisposition string `json:"content_disposition"`
}
type certificateDownloadResponse struct{ output CertificateDownload }

func (*certificateDownloadResponse) acceptStatus(int) bool { return false }
func (r *certificateDownloadResponse) decodeResponse(data []byte, headers http.Header, _ int) error {
	r.output = CertificateDownload{DataBase64: base64.StdEncoding.EncodeToString(data), ContentType: headers.Get("Content-Type"), ContentDisposition: headers.Get("Content-Disposition")}
	return nil
}

// Clone the HTTP client so certificate operations do not alter ordinary request
// deadlines or disable cancellation, response bounds or redirect refusal.
func (c *Client) withNginxTimeout(timeout time.Duration) *Client {
	copy := *c
	transport := *c.http
	transport.Timeout = timeout
	copy.http = &transport
	return &copy
}

func (c *Client) ListCertificates(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/certs/list")
}

func (c *Client) ListDNSProviders(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/certs/dns-providers")
}

func (c *Client) DownloadCertificate(ctx context.Context, input NginxIDInput) (CertificateDownload, error) {
	var result certificateDownloadResponse
	if input.ID < 1 {
		return CertificateDownload{}, errors.New("id must be positive")
	}
	slow := c.withNginxTimeout(3 * time.Minute)
	if err := slow.request(ctx, "POST", "/certs/download", input, &result); err != nil {
		return CertificateDownload{}, err
	}
	return result.output, nil
}

func (c *Client) RenewCertificate(ctx context.Context, input NginxIDInput) error {
	slow := c.withNginxTimeout(11 * time.Minute)
	return slow.request(ctx, "POST", "/certs/renew", input, nil)
}

func (c *Client) ListAccessLists(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/access-lists/list")
}

func (c *Client) CreateAccessList(ctx context.Context, input AccessListConfig) error {
	return c.request(ctx, "POST", "/access-lists/create", input, nil)
}

func (c *Client) ListStreams(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/stream/list")
}

func (c *Client) CreateStream(ctx context.Context, input StreamConfig) error {
	return c.request(ctx, "POST", "/stream/create", input, nil)
}

func (c *Client) UpdateStream(ctx context.Context, input UpdateStreamConfig) error {
	return c.request(ctx, "PUT", "/stream/edit", input, nil)
}

func (c *Client) EnableStream(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/stream/enable", input, nil)
}

func (c *Client) DisableStream(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/stream/disable", input, nil)
}

func (c *Client) ListRedirects(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/redirection/list")
}

func (c *Client) CreateRedirect(ctx context.Context, input RedirectConfig) error {
	return c.request(ctx, "POST", "/redirection/create", input, nil)
}

func (c *Client) UpdateRedirect(ctx context.Context, input UpdateRedirectConfig) error {
	return c.request(ctx, "PUT", "/redirection/edit", input, nil)
}

func (c *Client) EnableRedirect(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/redirection/enable", input, nil)
}

func (c *Client) DisableRedirect(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/redirection/disable", input, nil)
}

func (c *Client) ListHosts404(ctx context.Context) ([]map[string]any, error) {
	return c.listNginxObjects(ctx, "/404/list")
}

func (c *Client) CreateHost404(ctx context.Context, input Host404Config) error {
	return c.request(ctx, "POST", "/404/create", input, nil)
}

func (c *Client) UpdateHost404(ctx context.Context, input UpdateHost404Config) error {
	return c.request(ctx, "PUT", "/404/edit", input, nil)
}

func (c *Client) EnableHost404(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/404/enable", input, nil)
}

func (c *Client) DisableHost404(ctx context.Context, input NginxIDInput) error {
	return c.request(ctx, "POST", "/404/disable", input, nil)
}
