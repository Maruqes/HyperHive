package api

import (
	"os"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/go-chi/chi/v5"
)

// MCP intentionally has no caller authentication for this initial increment.
// Only its server-side request to HyperHive's protected API carries a token.
func setupMCPAPI(r chi.Router, port string) {
	r.Handle("/mcp", hyperhivemcp.NewHTTPHandler(hyperhivemcp.Config{
		BaseURL: "http://127.0.0.1:" + port,
		Token:   os.Getenv("HYPERHIVE_API_TOKEN"),
	}))
}
