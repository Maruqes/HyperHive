package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUnauthenticatedHTTPContract(t *testing.T) {
	var backendCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/proxy/list" || r.Header.Get("Authorization") != "Bearer server-token" {
			t.Error("backend request did not use the configured API contract")
		}
		fmt.Fprint(w, `[{"id":1,"domain_names":["http.example.test"],"enabled":false}]`)
	}))
	defer backend.Close()
	for _, token := range []string{"server-token", ""} {
		t.Run("token="+fmt.Sprint(token != ""), func(t *testing.T) {
			mux := http.NewServeMux()
			mux.Handle("/mcp", NewHTTPHandler(Config{BaseURL: backend.URL, Token: token}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("MCP callers should not need credentials")
				}
				mux.ServeHTTP(w, r)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := mcp.NewClient(&mcp.Implementation{Name: "http-contract"}, nil).Connect(ctx,
				&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			list, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(list.Tools) != 1 || list.Tools[0].Name != "list_proxies" {
				t.Fatalf("expected only list_proxies, got %#v", list.Tools)
			}
			before := backendCalls.Load()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies", Arguments: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			if token == "" {
				if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "HYPERHIVE_API_TOKEN") || backendCalls.Load() != before {
					t.Fatalf("missing backend token should yield a configuration tool error: %#v", result)
				}
			} else {
				data, _ := json.Marshal(result.StructuredContent)
				if result.IsError || !strings.Contains(string(data), "http.example.test") || backendCalls.Load() != before+1 {
					t.Fatalf("unexpected HTTP tool result: %#v", result)
				}
			}
			resp, err := http.Get(server.URL + "/mcp")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("stateless GET should return 405, got %d", resp.StatusCode)
			}
		})
	}
}
