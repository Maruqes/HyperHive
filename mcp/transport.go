package hyperhivemcp

import (
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Modern clients use stateless multi-round-trip approval. Legacy clients need
// a retained session so the form picker can answer server elicitation. Both
// handlers share the exact registry and remain behind the same Bearer check.
func newMCPTransport(server *mcp.Server) http.Handler {
	getServer := func(*http.Request) *mcp.Server { return server }
	modern := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true})
	legacy := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{SessionTimeout: 20 * time.Minute})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Header.Get("MCP-Protocol-Version")
		if version >= "2026-07-28" || ((r.Method == http.MethodGet || r.Method == http.MethodDelete) && r.Header.Get("Mcp-Session-Id") == "") {
			modern.ServeHTTP(w, r)
			return
		}
		legacy.ServeHTTP(w, r)
	})
}
