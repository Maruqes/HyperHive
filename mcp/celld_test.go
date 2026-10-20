package hyperhivemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type celldCase struct {
	wantResponse                 string
	headers                      http.Header
	name, method, path, response string
	args                         map[string]any
	body                         any
	query                        url.Values
	invalid                      []map[string]any
	status                       int
}

// Expectations below are independent fixtures from the API handlers and service types.
func runCelldContract(t *testing.T, tc celldCase) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "hyperhive-mcp")
	if out, err := exec.Command("go", "build", "-o", binary, "./cmd/hyperhive-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var mu sync.Mutex
			calls, status, response := 0, tc.status, tc.response
			headers := tc.headers
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if r.Method != tc.method || r.URL.EscapedPath() != "/prefix/api"+tc.path || !reflect.DeepEqual(r.URL.Query(), tc.query) {
					t.Errorf("wire got %s %s, want %s %s query %v", r.Method, r.URL, tc.method, tc.path, tc.query)
				}
				if r.Header.Get("Authorization") != "Bearer celld-fixture-key" || !strings.Contains(r.Header.Get("Accept"), "application/json") {
					t.Error("missing auth/accept")
				}
				raw, _ := io.ReadAll(r.Body)
				if tc.body == nil {
					if len(raw) != 0 || r.Header.Get("Content-Type") != "" {
						t.Errorf("unexpected request body/header: %q", raw)
					}
				} else {
					var got any
					if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, tc.body) || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("body: %s want %#v", raw, tc.body)
					}
				}
				for key, values := range headers {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				w.WriteHeader(status)
				fmt.Fprint(w, response)
			}))
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			c := mcp.NewClient(&mcp.Implementation{Name: "celld-contract"}, nil)
			var session *mcp.ClientSession
			var err error
			var stderr bytes.Buffer
			if transport == "http" {
				endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "celld-fixture-key"}))
				defer endpoint.Close()
				session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"celld-fixture-key"}}}, nil)
			} else {
				cmd := exec.CommandContext(ctx, binary, "serve")
				cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=celld-fixture-key")
				cmd.Stderr = &stderr
				session, err = c.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			list, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var found *mcp.Tool
			for _, tool := range list.Tools {
				if tool.Name == tc.name {
					found = tool
				}
			}
			if found == nil {
				t.Fatalf("missing %s", tc.name)
			}
			mapping, _ := found.Meta["hyperhive/endpoint"].(map[string]any)
			wantPath := strings.Replace(tc.path, "fleet_one", "{id}", 1)
			read := tc.method == "GET"
			if mapping["method"] != tc.method || mapping["path"] != wantPath || found.InputSchema == nil || found.OutputSchema == nil || found.Annotations == nil || found.Annotations.ReadOnlyHint != read || found.Annotations.DestructiveHint == nil || *found.Annotations.DestructiveHint != (tc.name == "delete_celld_fleet") {
				t.Fatalf("discovery: %#v", found)
			}
			call := func(args map[string]any) *mcp.CallToolResult {
				t.Helper()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
				if e != nil {
					t.Fatal(e)
				}
				return result
			}
			checkSuccess := func(want string) {
				t.Helper()
				result := call(tc.args)
				if result.IsError {
					t.Fatalf("call failed: %#v", result)
				}
				data, _ := json.Marshal(result.StructuredContent)
				var got, expected any
				json.Unmarshal(data, &got)
				json.Unmarshal([]byte(`{"result":`+want+`}`), &expected)
				if strings.Contains(want, "1792310400123456789") && !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "1792310400123456789") {
					t.Fatal("lost integer precision in text fallback")
				}
				if strings.Contains(want, "1792310400123456789") && !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "1792310400123456789") {
					t.Fatal("lost integer precision in text fallback")
				}
				if !reflect.DeepEqual(got, expected) || len(result.Content) != 1 {
					t.Fatalf("result: %s want %s", data, want)
				}
				var textValue any
				if json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &textValue) != nil || !reflect.DeepEqual(got, textValue) {
					t.Fatal("text fallback differs")
				}
			}
			wantResponse := tc.response
			if tc.wantResponse != "" {
				wantResponse = tc.wantResponse
			}
			checkSuccess(wantResponse)
			invalid := append([]map[string]any{changed(tc.args, "unknown", true)}, tc.invalid...)
			for key := range tc.args {
				invalid = append(invalid, changed(tc.args, key, nil))
			}
			for _, args := range invalid {
				mu.Lock()
				before := calls
				mu.Unlock()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
				if e == nil && !result.IsError {
					t.Errorf("invalid input accepted: %#v", args)
				}
				mu.Lock()
				after := calls
				mu.Unlock()
				if after != before {
					t.Errorf("invalid input reached API: %#v", args)
				}
			}
			for _, code := range []int{400, 401, 403, 404, 409, 500, 503} {
				if tc.name == "get_celld_health" && code == 503 {
					continue
				}
				mu.Lock()
				status = code
				response = "celld-fixture-key private backend body"
				before := calls
				mu.Unlock()
				result := call(tc.args)
				if !result.IsError || len(result.Content) == 0 {
					t.Fatalf("HTTP %d accepted", code)
				}
				message := result.Content[0].(*mcp.TextContent).Text
				if !strings.Contains(message, fmt.Sprintf("HTTP %d", code)) || strings.Contains(message, "celld-fixture-key") || strings.Contains(message, "private backend") {
					t.Fatalf("unsafe error %q", message)
				}
				mu.Lock()
				after := calls
				mu.Unlock()
				if after != before+1 {
					t.Fatal("request automatically retried")
				}
			}
			for _, bad := range []string{"", "{", "[]", "null", "{}", `{"error":"celld-fixture-key"}`} {
				if tc.wantResponse != "" && bad == "" {
					continue
				}
				if tc.wantResponse != "" && bad == "" {
					continue
				}
				mu.Lock()
				status = tc.status
				response = bad
				mu.Unlock()
				result := call(tc.args)
				if !result.IsError {
					t.Errorf("bad response accepted: %q", bad)
				}
				data, _ := json.Marshal(result)
				if strings.Contains(string(data), "celld-fixture-key") {
					t.Fatal("response leaked through error")
				}
			}
			if tc.name == "get_celld_health" {
				badHealth := `{"healthy":false,"storage":{"healthy":false,"error":"not checked"},"fleets":[],"machines":[],"checked_at":"2026-10-18T00:00:00Z"}`
				mu.Lock()
				status = 503
				response = badHealth
				mu.Unlock()
				checkSuccess(badHealth)
				mu.Lock()
				response = tc.response
				mu.Unlock()
				if !call(tc.args).IsError {
					t.Fatal("503 healthy=true accepted")
				}
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected executable diagnostics: %s", stderr.String())
			}
		})
	}
}

func TestCelldList(t *testing.T) {
	runCelldContract(t, celldCase{name: "list_celld_fleets", method: "GET", path: "/celld/fleets", args: map[string]any{}, query: url.Values{}, status: 200, response: `{"fleets":[],"storage":{"healthy":false,"bucket_count":null,"buckets":null,"managed_bucket_count":0,"checked_at":"2026-10-18T00:00:00Z","error":"not checked"},"reset_pending":false,"error":"partial state"}`})
}

func TestCelldHealth(t *testing.T) {
	runCelldContract(t, celldCase{name: "get_celld_health", method: "GET", path: "/celld/health", args: map[string]any{}, query: url.Values{}, status: 200, response: `{"healthy":true,"storage":{"healthy":true},"fleets":[],"machines":[],"checked_at":"2026-10-18T00:00:00Z"}`})
}

func TestCelldCreate(t *testing.T) {
	args := map[string]any{"name": "analytics", "servers": []any{"node-a", "node-b"}, "ports": map[string]any{"http": float64(8080), "internal": float64(8081)}}
	runCelldContract(t, celldCase{name: "create_celld_fleet", method: "POST", path: "/celld/fleets", args: args, body: args, query: url.Values{}, status: 202, response: `{"id":"fleet_one","name":"analytics","desired_state":"running","status":"provisioning","servers":[],"ports":{"http":8080,"internal":8081},"storage":{"bucket":"hyperhive-celld-fleet_one","region":"us-east-1","endpoint":""},"urls":[],"deploy":{"command":"","credentials_url":"/celld/fleets/fleet_one/credentials","environment":[]},"checked_at":"2026-10-18T00:00:00Z"}`, invalid: []map[string]any{
		{}, changed(args, "name", ""), changed(args, "name", " analytics"), changed(args, "name", strings.Repeat("x", 129)), changed(args, "servers", []any{}), changed(args, "servers", []any{"node-a", "node-a"}), changed(args, "servers", []any{""}), changed(args, "ports", map[string]any{"http": 0, "internal": 8081}), changed(args, "ports", map[string]any{"http": 8080, "internal": 8080}), changed(args, "ports", map[string]any{"http": 65536, "internal": 8081}), changed(args, "ports", map[string]any{"http": 8080}), changed(args, "ports", map[string]any{"http": 8080, "internal": 8081, "other": 1}),
	}})
}

func TestCelldStart(t *testing.T) {
	args := map[string]any{"id": "fleet_one"}
	runCelldContract(t, celldCase{name: "start_celld_fleet", method: "POST", path: "/celld/fleets/fleet_one/start", args: args, query: url.Values{}, status: 202, response: `{"id":"fleet_one","name":"analytics","desired_state":"running","status":"provisioning","servers":[],"ports":{"http":8080,"internal":8081},"storage":{"bucket":"hyperhive-celld-fleet_one","region":"us-east-1","endpoint":""},"urls":[],"deploy":{"command":"","credentials_url":"/celld/fleets/fleet_one/credentials","environment":[]},"checked_at":"2026-10-18T00:00:00Z"}`, invalid: []map[string]any{{}, changed(args, "id", ""), changed(args, "id", "../other"), changed(args, "id", "a/b"), changed(args, "id", "a?b"), changed(args, "id", "a#b"), changed(args, "id", "a%2fb"), changed(args, "id", strings.Repeat("x", 65)), changed(args, "id", 1)}})
}

func TestCelldStop(t *testing.T) {
	args := map[string]any{"id": "fleet_one"}
	runCelldContract(t, celldCase{name: "stop_celld_fleet", method: "POST", path: "/celld/fleets/fleet_one/stop", args: args, query: url.Values{}, status: 202, response: `{"id":"fleet_one","name":"analytics","desired_state":"stopped","status":"provisioning","servers":[],"ports":{"http":8080,"internal":8081},"storage":{"bucket":"hyperhive-celld-fleet_one","region":"us-east-1","endpoint":""},"urls":[],"deploy":{"command":"","credentials_url":"/celld/fleets/fleet_one/credentials","environment":[]},"checked_at":"2026-10-18T00:00:00Z"}`, invalid: []map[string]any{{}, changed(args, "id", ""), changed(args, "id", "../other"), changed(args, "id", "a/b"), changed(args, "id", "a?b"), changed(args, "id", "a#b"), changed(args, "id", "a%2fb"), changed(args, "id", strings.Repeat("x", 65)), changed(args, "id", 1)}})
}

func TestCelldDelete(t *testing.T) {
	args := map[string]any{"id": "fleet_one"}
	runCelldContract(t, celldCase{name: "delete_celld_fleet", method: "DELETE", path: "/celld/fleets/fleet_one", args: args, query: url.Values{}, status: 202, response: `{"id":"fleet_one","name":"analytics","desired_state":"deleting","status":"deleting","servers":[],"ports":{"http":8080,"internal":8081},"storage":{"bucket":"hyperhive-celld-fleet_one","region":"us-east-1","endpoint":""},"urls":[],"deploy":{"command":"","credentials_url":"/celld/fleets/fleet_one/credentials","environment":[]},"checked_at":"2026-10-18T00:00:00Z"}`, invalid: []map[string]any{{}, changed(args, "id", ""), changed(args, "id", "../other"), changed(args, "id", "a/b"), changed(args, "id", "a?b"), changed(args, "id", "a#b"), changed(args, "id", "a%2fb"), changed(args, "id", strings.Repeat("x", 65)), changed(args, "id", 1)}})
}

func TestCelldCredentials(t *testing.T) {
	args := map[string]any{"id": "fleet_one"}
	runCelldContract(t, celldCase{name: "get_celld_credentials", method: "GET", path: "/celld/fleets/fleet_one/credentials", args: args, query: url.Values{}, status: 200, response: `{"aws_access_key_id":"fleet-scoped-key","aws_secret_access_key":"fixture-secret-intentionally-returned"}`, invalid: []map[string]any{{}, changed(args, "id", ""), changed(args, "id", "../other"), changed(args, "id", 1)}})
}

func TestCelldLogs(t *testing.T) {
	args := map[string]any{"id": "fleet_one", "from": float64(1792310400000), "to": float64(1792314000000), "limit": float64(2), "cursor": "opaque+/=", "node": "node-a", "workload": "api/jobs", "source": "process", "severity": "warn", "text": "a & b?ü", "download": false}
	query := url.Values{"from": {"1792310400000"}, "to": {"1792314000000"}, "limit": {"2"}, "cursor": {"opaque+/="}, "node": {"node-a"}, "workload": {"api/jobs"}, "source": {"process"}, "severity": {"warn"}, "text": {"a & b?ü"}, "download": {"false"}}
	runCelldContract(t, celldCase{name: "get_celld_logs", method: "GET", path: "/celld/fleets/fleet_one/logs", args: args, query: query, status: 200, response: `{"records":[{"id":"r1","node":"node-a","generation":"g1","received_ms":1792310400000,"record":{"timestamp_ns":1792310400123456789,"message":"fixture"}}],"from":1792310400000,"to":1792314000000,"next_cursor":"opaque-next","truncated":true,"incomplete":true,"error":"partial collection"}`, invalid: []map[string]any{
		{}, changed(args, "id", "../other"), changed(args, "limit", -1), changed(args, "limit", 501), changed(args, "limit", 1.5), changed(args, "from", "now"), changed(args, "from", float64(1792315000000)), changed(args, "download", "true"), changed(args, "text", strings.Repeat("x", 257)), changed(args, "node", strings.Repeat("x", 129)), changed(args, "cursor", strings.Repeat("x", 1025)), changed(args, "workload", strings.Repeat("x", 257)), changed(args, "source", strings.Repeat("x", 33)), changed(args, "severity", strings.Repeat("x", 17)),
	}})
}

func runCelldDownloads(t *testing.T, kind string) {
	t.Helper()
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			args := map[string]any{"id": "fleet_one", "download": true, "limit": float64(2000)}
			headers := http.Header{"Content-Type": {"application/x-ndjson"}}
			raw := `{"id":"r1","node":"node-a","generation":"g","received_ms":1792310400000,"record":{"timestamp_ns":1792310400123456789}}` + "\n" + `{"id":"r2","node":"node-b","generation":"g","received_ms":1792310400001}` + "\n"
			want := `{"records":[{"id":"r1","node":"node-a","generation":"g","received_ms":1792310400000,"record":{"timestamp_ns":1792310400123456789}},{"id":"r2","node":"node-b","generation":"g","received_ms":1792310400001}],"truncated":true,"next_cursor":"continue+opaque="}`
			if empty {
				raw = ""
				want = `{"records":[],"truncated":false}`
			} else {
				headers.Set("X-Observability-Truncated", "true")
				headers.Set("X-Next-Cursor", "continue+opaque=")
			}
			runCelldContract(t, celldCase{name: "get_celld_" + kind, method: "GET", path: "/celld/fleets/fleet_one/" + kind, args: args, query: url.Values{"download": {"true"}, "limit": {"2000"}}, status: 200, response: raw, wantResponse: want, headers: headers, invalid: []map[string]any{changed(args, "limit", 2001)}})
		})
	}
}
func TestCelldLogsDownloads(t *testing.T) { runCelldDownloads(t, "logs") }

func TestCelldEvents(t *testing.T) {
	args := map[string]any{"id": "fleet_one", "from": float64(1792310400000), "to": float64(1792314000000), "limit": float64(2), "cursor": "opaque+/=", "node": "node-a", "workload": "api/jobs", "source": "process", "severity": "warn", "text": "a & b?ü", "download": false}
	query := url.Values{"from": {"1792310400000"}, "to": {"1792314000000"}, "limit": {"2"}, "cursor": {"opaque+/="}, "node": {"node-a"}, "workload": {"api/jobs"}, "source": {"process"}, "severity": {"warn"}, "text": {"a & b?ü"}, "download": {"false"}}
	runCelldContract(t, celldCase{name: "get_celld_events", method: "GET", path: "/celld/fleets/fleet_one/events", args: args, query: query, status: 200, response: `{"records":[{"id":"r1","node":"node-a","generation":"g1","received_ms":1792310400000,"record":{"timestamp_ns":1792310400123456789,"message":"fixture"}}],"from":1792310400000,"to":1792314000000,"next_cursor":"opaque-next","truncated":true,"incomplete":true,"error":"partial collection"}`, invalid: []map[string]any{
		{}, changed(args, "id", "../other"), changed(args, "limit", -1), changed(args, "limit", 501), changed(args, "limit", 1.5), changed(args, "from", "now"), changed(args, "from", float64(1792315000000)), changed(args, "download", "true"), changed(args, "text", strings.Repeat("x", 257)), changed(args, "node", strings.Repeat("x", 129)), changed(args, "cursor", strings.Repeat("x", 1025)), changed(args, "workload", strings.Repeat("x", 257)), changed(args, "source", strings.Repeat("x", 33)), changed(args, "severity", strings.Repeat("x", 17)),
	}})
}

func TestCelldEventsDownloads(t *testing.T) { runCelldDownloads(t, "events") }

func TestCompleteToolRegistry(t *testing.T) {
	endpoint := httptest.NewServer(NewHTTPHandler(Config{MCPToken: "celld-fixture-key"}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "registry-test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"celld-fixture-key"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create_celld_fleet", "create_proxy", "delete_celld_fleet", "disable_proxy", "enable_proxy", "get_celld_credentials", "get_celld_events", "get_celld_health", "get_celld_logs", "list_celld_fleets", "list_proxies", "setup_frontend_proxy", "setup_secure_frontend_proxy", "start_celld_fleet", "stop_celld_fleet", "update_proxy"}
	for _, tc := range nginxCases {
		want = append(want, tc.Name)
	}
	want = append(want, selectedFeatureToolNames...)
	want = append(want, "list_upstreams", "get_upstream", "get_upstream_capabilities", "create_upstream", "update_upstream")
	sort.Strings(want)
	var got []string
	for _, tool := range list.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry got %v want %v", got, want)
	}
	for _, name := range []string{"delete_proxy", "delete_celld_fleets", "remove_celld_fleet", "edit_celld_fleet", "update_celld_fleet", "restart_celld_fleet", "call_api", "celld_action"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"id": "fleet_one"}})
		if err == nil && !result.IsError {
			t.Fatalf("unrequested tool exposed: %s", name)
		}
	}
}
