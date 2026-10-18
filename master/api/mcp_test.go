package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPublicRoutePreservesAPIAuthentication(t *testing.T) {
	t.Setenv("HYPERHIVE_API_TOKEN", "")
	router := chi.NewRouter()
	setupMCPAPI(router, "9595")
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/proxy/list", func(w http.ResponseWriter, r *http.Request) {
			t.Error("unauthenticated request reached the protected API handler")
		})
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "master-route-test"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("unauthenticated MCP connection: %v", err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "list_proxies" {
		t.Fatalf("unexpected tools: %#v", list.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies"})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "HYPERHIVE_API_TOKEN") {
		t.Fatalf("missing configuration should not prevent startup/discovery: %#v, %v", result, err)
	}
	resp, err := http.Get(server.URL + "/proxy/list")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("proxy API should still require authentication, got %d", resp.StatusCode)
	}
}

func TestMCPLoopbackUsesConfiguredPort(t *testing.T) {
	t.Setenv("HYPERHIVE_API_TOKEN", "server-token")
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer auth.Close()
	oldURL := baseURL
	baseURL = auth.URL
	t.Cleanup(func() { baseURL = oldURL })
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/proxy/list", func(w http.ResponseWriter, r *http.Request) {
			if GetTokenFromContext(r) != "server-token" {
				t.Error("backend authentication missing")
			}
			fmt.Fprint(w, `[{"id":7,"domain_names":["custom-port.test"]}]`)
		})
	})
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "custom-port-test"}, nil).Connect(ctx,
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
		t.Fatalf("expected exactly list_proxies: %#v", list.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies"})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "custom-port.test") {
		t.Fatalf("custom port invocation: %#v, %v", result, err)
	}
}

func TestConfiguredAPIPort(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"", "9595"}, {"  ", "9595"}, {"9595", "9595"}, {" 10595 ", "10595"}, {"65535", "65535"},
	} {
		got, err := configuredAPIPort(tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("port %q: got %q, %v", tc.value, got, err)
		}
	}
	for _, value := range []string{"0", "-1", "65536", "localhost:9595", "abc"} {
		if _, err := configuredAPIPort(value); err == nil {
			t.Fatalf("invalid port accepted: %q", value)
		}
	}
}
