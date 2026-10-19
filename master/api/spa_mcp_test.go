package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/internal/testnpm"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Real management handlers with a disposable database. No firewall mutations or
// ipset commands: the allows read deliberately targets an absent configured port.
func TestSPAMCPActualHandlers(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	database, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "spa.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = old; database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.CreateSPAPortsTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateResourceDescriptionsTable(ctx); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupSPAAPI(r) })
	server.Start()
	defer server.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "spa-handler-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	list := call("list_spa_ports", map[string]any{})
	if list.IsError || !strings.Contains(list.Content[0].(*mcp.TextContent).Text, `"spa_ports":[]`) {
		t.Fatalf("empty ports: %#v", list)
	}
	if err := db.UpsertSPAPort(ctx, 443, "fixture-private-password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetResourceDescription(ctx, spaPortResourceType, 443, "fixture description"); err != nil {
		t.Fatal(err)
	}
	list = call("list_spa_ports", map[string]any{})
	if list.IsError {
		t.Fatalf("populated ports: %#v", list)
	}
	text := list.Content[0].(*mcp.TextContent).Text
	if strings.Contains(strings.ToLower(text), "password") || strings.Contains(text, "fixture-private") {
		t.Fatal("password hash exposed")
	}
	var output struct {
		Result struct {
			Ports []struct {
				Port        int       `json:"port"`
				Description string    `json:"description"`
				CreatedAt   time.Time `json:"created_at"`
			} `json:"spa_ports"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Result.Ports) != 1 || output.Result.Ports[0].Port != 443 || output.Result.Ports[0].Description != "fixture description" || output.Result.Ports[0].CreatedAt.IsZero() {
		t.Fatalf("incorrect real handler result: %s", text)
	}
	missing := call("list_spa_allows", map[string]any{"port": 444})
	if !missing.IsError || !strings.Contains(missing.Content[0].(*mcp.TextContent).Text, "HTTP 401") || strings.Contains(missing.Content[0].(*mcp.TextContent).Text, "invalid credentials") {
		t.Fatalf("missing port safe mapping: %#v", missing)
	}
	for _, path := range []string{"/spa/allow/0", "/spa/allow/not-a-port"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer client-key")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("invalid port status: %d", response.StatusCode)
		}
	}
	if _, err := database.ExecContext(ctx, "DROP TABLE resource_descriptions"); err != nil {
		t.Fatal(err)
	}
	failure := call("list_spa_ports", map[string]any{})
	if !failure.IsError || !strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "HTTP 500") || strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "resource_descriptions") {
		t.Fatalf("database failure unsafe: %#v", failure)
	}
}
