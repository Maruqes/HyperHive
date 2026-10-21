// Package hyperhivemcp provides HyperHive's MCP server for embedding
// in the master HTTP API as well as running with the standalone stdio command.
package hyperhivemcp

import (
	"context"
	"net/http"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	BaseURL  string
	MCPToken string
	// VMBackupDir is a private operator-controlled directory for retained
	// pre-change records. Empty uses the MCP process user's cache directory.
	VMBackupDir string
	// SafetyReviewDir stores bounded private Medium preflight records.
	SafetyReviewDir string
}

func newServer(api tools.API, backupDir, reviewDir string) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "hyperhive-mcp", Version: "0.1.0"}, nil)
	registry := tools.NewRegistry(server, reviewDir)
	if err := tools.RegisterAll(registry, api, backupDir); err != nil {
		return nil, err
	}
	return server, nil
}

// NewServer validates the backend configuration for a standalone stdio server.
func NewServer(cfg Config) (*mcp.Server, error) {
	api, err := client.New(client.Config{BaseURL: cfg.BaseURL, Token: cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return newServer(api, cfg.VMBackupDir, cfg.SafetyReviewDir)
}

// NewHTTPHandler requires a fixed MCP Bearer token. Backend configuration is
// validated on tool invocation; a missing MCP key disables HTTP access.
func NewHTTPHandler(cfg Config) http.Handler {
	server, err := newServer(configuredAPI{cfg}, cfg.VMBackupDir, cfg.SafetyReviewDir)
	if err != nil {
		return requireMCPToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "MCP tool registration is unavailable", http.StatusServiceUnavailable)
		}), cfg.MCPToken)
	}
	handler := newMCPTransport(server)
	return requireMCPToken(handler, cfg.MCPToken)
}

type configuredAPI struct{ cfg Config }

func (api configuredAPI) ListProxies(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListProxies(ctx)
}

func (api configuredAPI) CreateProxy(ctx context.Context, input client.ProxyConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.CreateProxy(ctx, input)
}

func (api configuredAPI) UpdateProxy(ctx context.Context, input client.UpdateProxyInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.UpdateProxy(ctx, input)
}

func (api configuredAPI) EnableProxy(ctx context.Context, input client.ProxyIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.EnableProxy(ctx, input)
}

func (api configuredAPI) DisableProxy(ctx context.Context, input client.ProxyIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.DisableProxy(ctx, input)
}

func (api configuredAPI) SetupFrontendProxy(ctx context.Context, input client.FrontendProxyInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.SetupFrontendProxy(ctx, input)
}

func (api configuredAPI) SetupSecureFrontendProxy(ctx context.Context, input client.SecureFrontendProxyInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.SetupSecureFrontendProxy(ctx, input)
}

func (api configuredAPI) ListCelldFleets(ctx context.Context) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListCelldFleets(ctx)
}

func (api configuredAPI) GetCelldHealth(ctx context.Context) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetCelldHealth(ctx)
}

func (api configuredAPI) CreateCelldFleet(ctx context.Context, input client.CreateCelldFleetInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.CreateCelldFleet(ctx, input)
}

func (api configuredAPI) StartCelldFleet(ctx context.Context, input client.CelldFleetIDInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.StartCelldFleet(ctx, input)
}

func (api configuredAPI) StopCelldFleet(ctx context.Context, input client.CelldFleetIDInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.StopCelldFleet(ctx, input)
}

func (api configuredAPI) DeleteCelldFleet(ctx context.Context, input client.CelldFleetIDInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.DeleteCelldFleet(ctx, input)
}

func (api configuredAPI) GetCelldCredentials(ctx context.Context, input client.CelldFleetIDInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetCelldCredentials(ctx, input)
}

func (api configuredAPI) GetCelldLogs(ctx context.Context, input client.CelldObservationInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetCelldLogs(ctx, input)
}

func (api configuredAPI) GetCelldEvents(ctx context.Context, input client.CelldObservationInput) (client.CelldObject, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.GetCelldEvents(ctx, input)
}

func (api configuredAPI) ListCertificates(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListCertificates(ctx)
}

func (api configuredAPI) ListDNSProviders(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListDNSProviders(ctx)
}

func (api configuredAPI) DownloadCertificate(ctx context.Context, input client.NginxIDInput) (client.CertificateDownload, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return client.CertificateDownload{}, err
	}
	return c.DownloadCertificate(ctx, input)
}

func (api configuredAPI) RenewCertificate(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.RenewCertificate(ctx, input)
}

func (api configuredAPI) ListAccessLists(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListAccessLists(ctx)
}

func (api configuredAPI) CreateAccessList(ctx context.Context, input client.AccessListConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.CreateAccessList(ctx, input)
}

func (api configuredAPI) ListStreams(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListStreams(ctx)
}

func (api configuredAPI) CreateStream(ctx context.Context, input client.StreamConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.CreateStream(ctx, input)
}

func (api configuredAPI) UpdateStream(ctx context.Context, input client.UpdateStreamConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.UpdateStream(ctx, input)
}

func (api configuredAPI) EnableStream(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.EnableStream(ctx, input)
}

func (api configuredAPI) DisableStream(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.DisableStream(ctx, input)
}

func (api configuredAPI) ListRedirects(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListRedirects(ctx)
}

func (api configuredAPI) CreateRedirect(ctx context.Context, input client.RedirectConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.CreateRedirect(ctx, input)
}

func (api configuredAPI) UpdateRedirect(ctx context.Context, input client.UpdateRedirectConfig) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.UpdateRedirect(ctx, input)
}

func (api configuredAPI) EnableRedirect(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.EnableRedirect(ctx, input)
}

func (api configuredAPI) DisableRedirect(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.DisableRedirect(ctx, input)
}

func (api configuredAPI) ListHosts404(ctx context.Context) ([]map[string]any, error) {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return c.ListHosts404(ctx)
}

func (api configuredAPI) CreateHost404(ctx context.Context, input client.Host404Config) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.CreateHost404(ctx, input)
}

func (api configuredAPI) UpdateHost404(ctx context.Context, input client.UpdateHost404Config) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.UpdateHost404(ctx, input)
}

func (api configuredAPI) EnableHost404(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.EnableHost404(ctx, input)
}

func (api configuredAPI) DisableHost404(ctx context.Context, input client.NginxIDInput) error {
	c, err := client.New(client.Config{BaseURL: api.cfg.BaseURL, Token: api.cfg.MCPToken})
	if err != nil {
		return err
	}
	return c.DisableHost404(ctx, input)
}
