package api

import (
	"os"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/go-chi/chi/v5"
)

// MCP uses its own fixed Bearer key, independent of REST login sessions.
func setupMCPAPI(r chi.Router, port string) {
	r.Handle("/mcp", hyperhivemcp.NewHTTPHandler(hyperhivemcp.Config{
		BaseURL:     "http://127.0.0.1:" + port,
		MCPToken:    os.Getenv("MCP_TOKEN"),
		VMBackupDir: os.Getenv("MCP_VM_BACKUP_DIR"),
	}))
}
