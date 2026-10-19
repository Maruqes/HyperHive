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
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/internal/testnpm"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise real Notes handlers against an isolated SQLite database, with the
// fixed MCP authentication bridge and no production initialization or services.
func TestNotesMCPActualHandlers(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = fixture
	t.Cleanup(func() { db.DB = previous; fixture.Close() })
	if err := db.CreateNotesTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupNotesAPI(r) })
	server.Start()
	defer server.Close()
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer client-key")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s %s status %d want %d: %s", method, path, resp.StatusCode, status, data)
		}
		return data
	}
	if got := string(request("GET", "/notes/", "", 200)); got != "[]\n" && got != "[]" {
		t.Fatalf("initial list %s", got)
	}
	for _, body := range []string{`{}`, `{"titulo":"x"}`, `{"nota":"x"}`, `{"titulo":" ","nota":"x"}`, `{"titulo":"x","nota":" "}`, `{"titulo":false,"nota":"x"}`, `null`} {
		request("POST", "/notes/", body, 400)
	}
	created := request("POST", "/notes/", `{"titulo":" Title ","nota":"Body\nline"}`, 201)
	var note db.Note
	if json.Unmarshal(created, &note) != nil || note.ID != 1 || note.Titulo != " Title " || note.Nota != "Body\nline" {
		t.Fatalf("created note %s", created)
	}
	request("GET", "/notes/1", "", 200)
	request("GET", "/notes/999", "", 404)
	for _, id := range []string{"0", "-1", "text", "1.5"} {
		request("GET", "/notes/"+id, "", 400)
	}
	for _, method := range []string{"PUT", "PATCH"} {
		for _, body := range []string{`{"titulo":"x"}`, `{"nota":"x"}`, `{"titulo":"","nota":"x"}`, `{"titulo":"x","nota":"\t"}`} {
			request(method, "/notes/1", body, 400)
		}
		request(method, "/notes/0", `{"titulo":"x","nota":"x"}`, 400)
		request(method, "/notes/999", `{"titulo":"x","nota":"x"}`, 404)
		data := request(method, "/notes/1", `{"titulo":"`+method+` title","nota":"`+method+` body"}`, 200)
		if json.Unmarshal(data, &note) != nil || note.Titulo != method+" title" || note.Nota != method+" body" {
			t.Fatalf("replacement %s", data)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "notes-actual-handler"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any, wantError bool) json.RawMessage {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError != wantError {
			t.Fatalf("%s: %v %#v", name, err, result)
		}
		if wantError {
			return nil
		}
		data, _ := json.Marshal(result.StructuredContent)
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) != nil {
			t.Fatalf("invalid output %s", data)
		}
		return envelope["result"]
	}
	raw := call("create_note", map[string]any{"titulo": "MCP", "nota": "second note"}, false)
	if json.Unmarshal(raw, &note) != nil || note.ID != 2 {
		t.Fatalf("MCP create %s", raw)
	}
	call("get_note", map[string]any{"id": 2}, false)
	call("get_note", map[string]any{"id": 999}, true)
	for _, name := range []string{"replace_note", "patch_note"} {
		raw = call(name, map[string]any{"id": 2, "titulo": name, "nota": "replacement"}, false)
		if json.Unmarshal(raw, &note) != nil || note.Titulo != name || note.Nota != "replacement" {
			t.Fatalf("MCP replacement %s", raw)
		}
		call(name, map[string]any{"id": 2, "titulo": "partial"}, true)
	}
	var notes []db.Note
	raw = call("list_notes", map[string]any{}, false)
	if json.Unmarshal(raw, &notes) != nil || len(notes) != 2 || notes[0].ID != 2 || notes[1].ID != 1 {
		t.Fatalf("MCP list order %s", raw)
	}
}
