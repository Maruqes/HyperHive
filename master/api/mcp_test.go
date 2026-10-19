package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"512SvMan/api/npmapi"
	"512SvMan/internal/testnpm"
	"github.com/golang-jwt/jwt/v5"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpBearerTransport struct{}

func (mcpBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer client-key")
	return http.DefaultTransport.RoundTrip(req)
}

func TestMCPTokenPreservesAPIAuthentication(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
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
			t.Error("unauthenticated request reached the protected API handler")
		})
	})
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "master-route-test"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatalf("authenticated MCP connection: %v", err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 105 {
		t.Fatalf("unexpected tools: %#v", list.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies"})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "HTTP 503") {
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
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	key := testnpm.State(t, filepath.Join("npm-data"))
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "client-key" {
			t.Error("fixed MCP key reached NPM")
		}
		if token != "normal-user-session" {
			parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("api"), jwt.WithExpirationRequired())
			if err != nil || !parsed.Valid {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if parsed.Claims.(jwt.MapClaims)["attrs"].(map[string]any)["id"] != float64(7) {
				t.Error("wrong NPM service identity")
			}
		}
		if token == "normal-user-session" && r.URL.Path != "/api/users" && r.URL.Path != "/api/nginx/proxy-hosts" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/users" {
			fmt.Fprint(w, "[]")
			return
		}
		if r.URL.Path == "/api/nginx/proxy-hosts" {
			fmt.Fprint(w, `[{"id":7,"domain_names":["custom-port.test"]}]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer auth.Close()
	oldURL := baseURL
	baseURL = auth.URL
	npmapi.SetBaseURL(auth.URL)
	t.Cleanup(func() { baseURL = oldURL; npmapi.SetBaseURL(oldURL) })
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		npmapi.SetupProxyAPI(r)
	})
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "custom-port-test"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 105 {
		t.Fatalf("expected 105 HyperHive tools: %#v", list.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies"})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "custom-port.test") {
		t.Fatalf("custom port invocation: %#v, %v", result, err)
	}
	for _, tc := range []struct {
		header, cookie, query string
		want                  int
	}{
		{header: "Bearer client-key", want: 200},
		{header: "Bearer normal-user-session", want: 200},
		{header: "Bearer wrong-key", want: 401},
		{cookie: "client-key", want: 401},
		{query: "?token=client-key", want: 401},
		{cookie: "normal-user-session", want: 200},
		{query: "?token=normal-user-session", want: 200},
	} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/proxy/list"+tc.query, nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "Authorization", Value: tc.cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("API credential boundary: want %d, got %d", tc.want, resp.StatusCode)
		}
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
