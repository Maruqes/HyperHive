package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
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

// Real handlers and auth bridge use an isolated database and no slave connections.
// Successful asynchronous download is intentionally confined to HTTP fixtures.
func TestISOsMCPActualHandlers(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = previous; database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.CreateISOTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNFSTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.AddNFSShare(ctx, "iso-disconnected-fixture", "/fixture", "fixture:/source", "/isos", "Fixture", false); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupISOAPI(r) })
	server.Start()
	defer server.Close()
	request := func(body string, status int) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/isos/download", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer client-key")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("download status %d want %d: %s", response.StatusCode, status, data)
		}
	}
	for _, body := range []string{`{}`, `{"url":"x.ISO","iso_name":"a.iso","nfs_share_id":1}`, `{"url":"x.iso","iso_name":"a.ISO","nfs_share_id":1}`, `{"url":false}`} {
		request(body, 400)
	}
	// Missing share lookup maps sql.ErrNoRows to nil, and the handler returns 404.
	request(`{"url":"https://fixture.test/a.iso","iso_name":"a.iso","nfs_share_id":999}`, 404)
	request(`{"url":"https://fixture.test/a.iso","iso_name":"a.iso","nfs_share_id":1}`, 500)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "isos-actual-handler"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
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
	empty := call("list_isos", map[string]any{})
	if empty.IsError || empty.Content[0].(*mcp.TextContent).Text != `{"result":[]}` {
		t.Fatalf("empty list %#v", empty)
	}
	if err := db.AddISO(ctx, "iso-disconnected-fixture", "/isos/a.iso", "a.iso"); err != nil {
		t.Fatal(err)
	}
	populated := call("list_isos", map[string]any{})
	if populated.IsError {
		t.Fatalf("populated list %#v", populated)
	}
	var output struct {
		Result []struct {
			ID                          int `json:"Id"`
			MachineName, FilePath, Name string
			Available                   map[string]bool `json:"available_on_slaves"`
		} `json:"result"`
	}
	text := populated.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Result) != 1 || output.Result[0].ID != 1 || output.Result[0].MachineName != "iso-disconnected-fixture" || output.Result[0].FilePath != "/isos/a.iso" || output.Result[0].Name != "a.iso" || output.Result[0].Available == nil {
		t.Fatalf("wrong list %s", text)
	}
	request(`{"url":"https://fixture.test/a.iso","iso_name":"a.iso","nfs_share_id":1}`, 409)
	failure := call("start_iso_download", map[string]any{"url": "https://fixture.test/a.iso", "iso_name": "a.iso", "nfs_share_id": 1})
	if !failure.IsError || !strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "HTTP 409") || strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "already exists") {
		t.Fatalf("unsafe conflict %#v", failure)
	}
	failure = call("start_iso_download", map[string]any{"url": "https://fixture.test/b.iso", "iso_name": "b.iso", "nfs_share_id": 1})
	if !failure.IsError || !strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "HTTP 500") || strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "slave not connected") {
		t.Fatalf("unsafe disconnected error %#v", failure)
	}
	if _, err := database.ExecContext(ctx, "DROP TABLE isos"); err != nil {
		t.Fatal(err)
	}
	failure = call("list_isos", map[string]any{})
	if !failure.IsError || !strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "HTTP 500") || strings.Contains(failure.Content[0].(*mcp.TextContent).Text, "no such table") {
		t.Fatalf("unsafe database error %#v", failure)
	}
}
