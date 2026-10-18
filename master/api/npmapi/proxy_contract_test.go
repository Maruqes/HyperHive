package npmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type proxyFixtureBearer struct{}

func (proxyFixtureBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer fixture-key")
	return http.DefaultTransport.RoundTrip(r)
}

// Real HyperHive handlers and NPM HTTP helpers, with disposable downstream state.
// The API-auth bridge itself is exercised by master/api/mcp_test.go.
func TestMCPProxyMutationsThroughHandlers(t *testing.T) {
	oldURL, oldPort := baseURL, apiPort
	t.Cleanup(func() { baseURL, apiPort = oldURL, oldPort })
	apiPort = 10595
	var fixtureMu sync.Mutex
	proxies := `[]`
	var writes []map[string]any
	var requests []string
	failPublic := false
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixtureMu.Lock()
		defer fixtureMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture-session" {
			t.Error("wrong downstream identity")
		}
		if r.Method == "GET" {
			fmt.Fprint(w, proxies)
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == "DELETE" {
			t.Error("deletion must never reach NPM")
			w.WriteHeader(500)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/enable") || strings.HasSuffix(r.URL.Path, "/disable") {
			w.WriteHeader(200)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		writes = append(writes, body)
		if failPublic && body["domain_names"].([]any)[0] == "public.test" {
			http.Error(w, "fixture-secret", 500)
			return
		}
		fmt.Fprint(w, `{"id":42}`)
	}))
	defer downstream.Close()
	baseURL = downstream.URL
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-key" {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "token", "fixture-session")))
		})
	})
	SetupProxyAPI(router)
	api := httptest.NewServer(router)
	defer api.Close()
	endpoint := httptest.NewServer(hyperhivemcp.NewHTTPHandler(hyperhivemcp.Config{BaseURL: api.URL, MCPToken: "fixture-key"}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "actual-handler-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: proxyFixtureBearer{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	call := func(name string, args map[string]any, wantError bool) {
		fixtureMu.Unlock()
		defer fixtureMu.Lock()
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError != wantError {
			t.Fatalf("%s: %#v %v", name, result, err)
		}
		if wantError && strings.Contains(result.Content[0].(*mcp.TextContent).Text, "fixture-secret") {
			t.Fatal("backend response leaked")
		}
	}
	config := map[string]any{
		"domain_names": []any{"ordinary.test"}, "forward_scheme": "http", "forward_host": "127.0.0.1", "forward_port": 8080,
		"caching_enabled": false, "block_exploits": true, "allow_websocket_upgrade": true, "access_list_id": 0, "certificate_id": 0,
		"meta": map[string]any{"nested": map[string]any{"flag": false}}, "advanced_config": "", "locations": []any{map[string]any{"path": "/app", "forward_scheme": "https", "forward_host": "upstream.test", "forward_port": 443}},
		"http2_support": false, "hsts_enabled": false, "hsts_subdomains": false, "ssl_forced": false,
	}
	call("create_proxy", config, false)
	want := map[string]any{}
	for key, value := range config {
		want[key] = value
	}
	want["access_list_id"] = "0"
	data, _ := json.Marshal(want)
	json.Unmarshal(data, &want)
	if len(writes) != 1 || !reflect.DeepEqual(writes[0], want) {
		t.Fatalf("create fields changed: %#v", writes)
	}
	config["id"] = 42
	call("update_proxy", config, false)
	if len(writes) != 2 || !reflect.DeepEqual(writes[1], want) || requests[1] != "PUT /api/nginx/proxy-hosts/42" {
		t.Fatalf("update fields/path changed: %#v %v", writes, requests)
	}
	proxies = `[{"id":42,"domain_names":["ordinary.test"],"meta":{"managed_by":"setup_frontend"}}]`
	before := len(requests)
	call("update_proxy", config, true)
	call("disable_proxy", map[string]any{"id": 42}, true)
	if len(requests) != before {
		t.Fatal("managed restrictions allowed downstream writes")
	}
	call("enable_proxy", map[string]any{"id": 42}, false)
	proxies = `[]`
	call("disable_proxy", map[string]any{"id": 42}, false)
	call("setup_frontend_proxy", map[string]any{"domain": "frontend.test", "id": 0}, false)
	frontend := writes[len(writes)-1]
	if frontend["ssl_forced"] != false || frontend["certificate_id"] != float64(0) || frontend["forward_port"] != float64(8079) || frontend["meta"].(map[string]any)["managed_by"] != "setup_frontend" {
		t.Fatalf("frontend configuration: %#v", frontend)
	}
	for _, loc := range frontend["locations"].([]any) {
		if loc.(map[string]any)["forward_port"] != float64(10595) {
			t.Fatal("API_PORT lost")
		}
	}
	secure := map[string]any{"publicDomain": "public.test", "publicCertificateId": 0, "privateDomain": "private.test", "privateCertificateId": 9}
	call("setup_secure_frontend_proxy", secure, false)
	private, public := writes[len(writes)-2], writes[len(writes)-1]
	if private["domain_names"].([]any)[0] != "private.test" || private["ssl_forced"] != true || private["certificate_id"] != float64(9) || len(private["locations"].([]any)) != 3 {
		t.Fatalf("private configuration: %#v", private)
	}
	if public["ssl_forced"] != false || len(public["locations"].([]any)) != 1 || !strings.Contains(public["advanced_config"].(string), "return 403;") {
		t.Fatalf("public configuration: %#v", public)
	}
	// Existing managed IDs must be reused even when their domains change.
	proxies = `[{"id":11,"domain_names":["old-private.test"],"meta":{"managed_by":"setup_frontend"},"locations":[{"path":"/api"}]},{"id":22,"domain_names":["old-public.test"],"meta":{"managed_by":"setup_frontend"},"locations":[{"path":"/guest_api"}]}]`
	before = len(requests)
	call("setup_secure_frontend_proxy", secure, false)
	if !reflect.DeepEqual(requests[before:], []string{"PUT /api/nginx/proxy-hosts/11", "PUT /api/nginx/proxy-hosts/22"}) {
		t.Fatalf("managed IDs not reused: %v", requests[before:])
	}
	proxies = `[{"id":31,"domain_names":["frontend.test"]}]`
	before = len(requests)
	call("setup_frontend_proxy", map[string]any{"domain": "frontend.test", "id": 9}, false)
	if !reflect.DeepEqual(requests[before:], []string{"PUT /api/nginx/proxy-hosts/31"}) || writes[len(writes)-1]["ssl_forced"] != true {
		t.Fatal("single frontend update/certificate contract changed")
	}
	proxies = `[]`
	failPublic = true
	before = len(writes)
	call("setup_secure_frontend_proxy", secure, true)
	if len(writes) != before+2 || writes[before]["domain_names"].([]any)[0] != "private.test" {
		t.Fatal("partial failure ordering/retry contract changed")
	}
}
