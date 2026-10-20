package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUpstreamContractsBothTransports(t *testing.T) {
	config := map[string]any{"name": "app_pool", "context": "http", "loadbalancer_type": "round_robin", "server_arr": []any{map[string]any{"ip": "127.0.0.1", "port": 8080, "settings_string": "weight=2"}}}
	for _, tc := range []struct {
		name, method, path, response string
		args                         map[string]any
	}{
		{"list_upstreams", "GET", "/upstream/list", `[{"id":42,"revision":1,"apply_status":"applied","future_counter":9007199254740993}]`, map[string]any{}},
		{"get_upstream", "GET", "/upstream/42", `{"id":42,"revision":1,"future_counter":9007199254740993}`, map[string]any{"id": 42}},
		{"get_upstream_capabilities", "GET", "/upstream/capabilities", `{"status":"unknown","contexts":{"http":[],"stream":[]}}`, map[string]any{}},
		{"create_upstream", "POST", "/upstream/create", `{"id":42,"revision":1,"apply_status":"applied"}`, config},
		{"update_upstream", "PUT", "/upstream/42", `{"id":42,"revision":2,"apply_status":"applied"}`, changed(changed(config, "id", 42), "revision", 1)},
	} {
		for _, transport := range []string{"http", "stdio"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				var mu sync.Mutex
				calls := 0
				status := 200
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					if r.Method != tc.method || r.URL.Path != "/prefix/api"+tc.path || r.Header.Get("Authorization") != "Bearer upstream-fixture-key" {
						t.Errorf("wire %s %s", r.Method, r.URL)
					}
					if tc.method != "GET" {
						var got any
						if json.NewDecoder(r.Body).Decode(&got) != nil {
							t.Error("invalid body")
						}
						expected := tc.args
						if tc.method == "PUT" {
							expected = changed(tc.args, "id", 42)
						}
						raw, _ := json.Marshal(expected)
						var want any
						_ = json.Unmarshal(raw, &want)
						if !reflect.DeepEqual(got, want) {
							t.Errorf("got %v want %v", got, want)
						}
					}
					w.WriteHeader(status)
					if status == 200 {
						fmt.Fprint(w, tc.response)
					} else {
						fmt.Fprint(w, "fixture-secret")
					}
				}))
				defer backend.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				c := mcp.NewClient(&mcp.Implementation{Name: "upstream-contract"}, nil)
				var session *mcp.ClientSession
				var err error
				if transport == "http" {
					endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "upstream-fixture-key"}))
					defer endpoint.Close()
					session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"upstream-fixture-key"}}}, nil)
				} else {
					cmd := exec.CommandContext(ctx, nginxBinary, "serve")
					cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=upstream-fixture-key")
					session, err = c.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				listed, err := session.ListTools(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, tool := range listed.Tools {
					if strings.Contains(tool.Name, "upstream") && strings.Contains(tool.Name, "delete") {
						t.Fatal("deletion tool exposed")
					}
					if tool.Name == tc.name {
						found = true
						if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations.ReadOnlyHint != (tc.method == "GET") {
							t.Fatal("invalid discovery")
						}
					}
				}
				if !found {
					t.Fatal("tool missing")
				}
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil || result.IsError {
					t.Fatalf("invocation: %v %#v", err, result)
				}
				if strings.Contains(tc.response, "9007199254740993") {
					raw := result.Content[0].(*mcp.TextContent).Text
					if !strings.Contains(raw, "9007199254740993") {
						t.Fatal("response precision lost")
					}
				}
				mu.Lock()
				status = 503
				before := calls
				mu.Unlock()
				result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil || !result.IsError {
					t.Fatal("failure hidden")
				}
				if strings.Contains(result.Content[0].(*mcp.TextContent).Text, "fixture-secret") {
					t.Fatal("response secret leaked")
				}
				mu.Lock()
				after := calls
				mu.Unlock()
				if after != before+1 {
					t.Fatal("operation retried")
				}
			})
		}
	}
}

func TestPooledConsumerMCPContracts(t *testing.T) {
	proxy := configuration()
	proxy["upstream_id"] = 42
	proxy["forward_host"] = ""
	proxy["forward_port"] = 0
	proxy["locations"] = []any{map[string]any{"path": "/app", "forward_scheme": "https", "forward_host": "", "forward_port": 0, "upstream_id": 42, "advanced_config": "proxy_read_timeout 60s;"}}
	runMutationContract(t, mutationCase{name: "create_proxy", method: "POST", path: "/proxy/create", args: proxy})
	detach := configuration()
	detach["id"] = 42
	detach["upstream_id"] = nil
	runMutationContract(t, mutationCase{name: "update_proxy", method: "PUT", path: "/proxy/edit", args: detach})
	stream := map[string]any{"incoming_port": 6060, "forwarding_host": "", "forwarding_port": 0, "tcp_forwarding": true, "udp_forwarding": false, "certificate_id": 0, "meta": map[string]any{"dns_provider_credentials": "", "letsencrypt_agree": false, "dns_challenge": false}, "description": "pool", "upstream_id": 43}
	rawStream, _ := json.Marshal(stream)
	_ = json.Unmarshal(rawStream, &stream)
	runNginxContract(t, nginxCase{Name: "create_stream", Method: "POST", Path: "/stream/create", Kind: "mutation", Args: stream})
}
