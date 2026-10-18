package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type bearerTransport struct{ token string }

func (tr bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+tr.token)
	return http.DefaultTransport.RoundTrip(req)
}

func TestAuthenticatedHTTPContract(t *testing.T) {
	var backendCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/proxy/list" || r.Header.Get("Authorization") != "Bearer client-key" {
			t.Error("backend request must use the fixed MCP key")
		}
		fmt.Fprint(w, `[{"id":1,"domain_names":["http.example.test"],"enabled":false}]`)
	}))
	defer backend.Close()
	mux := http.NewServeMux()
	mux.Handle("/mcp", NewHTTPHandler(Config{BaseURL: backend.URL, MCPToken: "client-key"}))
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: bearerTransport{"client-key"}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "http-contract"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "list_proxies" {
		t.Fatalf("unexpected tools: %#v", list.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	if result.IsError || !strings.Contains(string(data), "http.example.test") || backendCalls.Load() != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	resp, err := http.Get(server.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", resp.StatusCode)
	}
	resp, err = httpClient.Get(server.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("authenticated stateless GET: %d", resp.StatusCode)
	}
}
