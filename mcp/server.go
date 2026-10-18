// Package hyperhivemcp provides HyperHive's single-tool MCP server for embedding
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
}

func newServer(api tools.ProxyLister) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "hyperhive-mcp", Version: "0.1.0"}, nil)
	tools.Register(server, api)
	return server
}

// NewServer validates the backend configuration for a standalone stdio server.
func NewServer(cfg Config) (*mcp.Server, error) {
	api, err := client.New(client.Config{BaseURL: cfg.BaseURL, Token: cfg.MCPToken})
	if err != nil {
		return nil, err
	}
	return newServer(api), nil
}

// NewHTTPHandler requires a fixed MCP Bearer token. Backend configuration is
// validated on tool invocation; a missing MCP key disables HTTP access.
func NewHTTPHandler(cfg Config) http.Handler {
	server := newServer(configuredAPI{cfg})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, PropagateRequestCancellation: true,
	})
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
