package hyperhivemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mutationCase struct {
	name, method, path string
	args               map[string]any
	invalid            []map[string]any
}

func configuration() map[string]any {
	return map[string]any{
		"domain_names":   []any{"proxy.example.test", "alias.example.test"},
		"forward_scheme": "https", "forward_host": "upstream.example.test", "forward_port": 443,
		"caching_enabled": false, "block_exploits": false, "allow_websocket_upgrade": true,
		"access_list_id": "0", "certificate_id": 0,
		"meta":            map[string]any{"letsencrypt_agree": false, "nested": map[string]any{"value": 42}},
		"advanced_config": "proxy_read_timeout 120s;",
		"locations":       []any{map[string]any{"path": "/app", "forward_scheme": "http", "forward_host": "127.0.0.1", "forward_port": 8080}},
		"http2_support":   false, "hsts_enabled": false, "hsts_subdomains": false, "ssl_forced": false,
	}
}

func changed(in map[string]any, key string, value any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	out[key] = value
	return out
}

func runMutationContract(t *testing.T, tc mutationCase) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "hyperhive-mcp")
	if out, err := exec.Command("go", "build", "-o", binary, "./cmd/hyperhive-mcp").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var calls atomic.Int32
			var status atomic.Int32
			status.Store(200)
			var wantBody atomic.Value
			encoded, _ := json.Marshal(tc.args)
			wantBody.Store(string(encoded))
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != tc.method || r.URL.Path != "/prefix/api"+tc.path || r.URL.RawQuery != "" {
					t.Errorf("wire: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
					t.Error("missing headers")
				}
				var got, want any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				json.Unmarshal([]byte(wantBody.Load().(string)), &want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body got %#v, want %#v", got, want)
				}
				w.WriteHeader(int(status.Load()))
				if status.Load() != 200 {
					fmt.Fprint(w, "fixture-key sensitive backend detail")
				}
			}))
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client := mcp.NewClient(&mcp.Implementation{Name: "proxy-contract"}, &mcp.ClientOptions{ElicitationHandler: vmApprove})
			var session *mcp.ClientSession
			var err error
			var stderr bytes.Buffer
			if transport == "http" {
				endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "fixture-key"}))
				defer endpoint.Close()
				session, err = client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"fixture-key"}}}, nil)
			} else {
				cmd := exec.CommandContext(ctx, binary, "serve")
				cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=fixture-key")
				cmd.Stderr = &stderr
				session, err = client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			listed, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantNames := []string{"create_proxy", "disable_proxy", "enable_proxy", "list_proxies", "setup_frontend_proxy", "setup_secure_frontend_proxy", "update_proxy"}
			var gotNames []string
			for _, tool := range listed.Tools {
				if strings.Contains(tool.Name, "proxy") || tool.Name == "list_proxies" {
					gotNames = append(gotNames, tool.Name)
				}
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, wantNames) {
				t.Fatalf("unexpected registry: %v", gotNames)
			}
			for _, forbidden := range []string{"delete_proxy", "remove_proxy", "purge_proxies", "call_api"} {
				before := calls.Load()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: forbidden, Arguments: map[string]any{"id": 42}})
				if err == nil && !result.IsError {
					t.Errorf("forbidden tool accepted: %s", forbidden)
				}
				if calls.Load() != before {
					t.Errorf("forbidden call reached backend: %s", forbidden)
				}
			}
			var found *mcp.Tool
			for _, tool := range listed.Tools {
				if tool.Name == tc.name {
					found = tool
				}
			}
			if found == nil {
				t.Fatalf("tool %s not registered", tc.name)
			}
			endpoint, ok := found.Meta["hyperhive/endpoint"].(map[string]any)
			if !ok || endpoint["method"] != tc.method || endpoint["path"] != tc.path {
				t.Fatalf("incorrect endpoint mapping: %#v", found.Meta)
			}
			if found.InputSchema == nil || found.OutputSchema == nil || found.Annotations == nil || found.Annotations.ReadOnlyHint || found.Annotations.DestructiveHint == nil || !*found.Annotations.DestructiveHint || found.Annotations.IdempotentHint {
				t.Fatalf("incorrect schemas/annotations: %#v", found)
			}
			valid := []map[string]any{tc.args}
			if _, ok := tc.args["access_list_id"]; ok {
				alternate := changed(tc.args, "access_list_id", 7)
				alternate["meta"] = map[string]any{}
				alternate["locations"] = []any{}
				alternate["advanced_config"] = ""
				for _, key := range []string{"caching_enabled", "block_exploits", "http2_support", "hsts_enabled", "hsts_subdomains", "ssl_forced"} {
					alternate[key] = true
				}
				valid = append(valid, alternate)
			}
			if tc.name == "setup_frontend_proxy" {
				valid = append(valid, changed(tc.args, "id", 9))
			}
			if tc.name == "setup_secure_frontend_proxy" {
				valid = append(valid, changed(tc.args, "privateCertificateId", 0))
			}
			for _, args := range valid {
				expected, _ := json.Marshal(args)
				wantBody.Store(string(expected))
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
				if err != nil || result.IsError {
					t.Fatalf("success: %#v %v", result, err)
				}
				data, _ := json.Marshal(result.StructuredContent)
				if string(data) != `{"success":true}` || len(result.Content) != 1 || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, `"success":true`) {
					t.Fatalf("invented or missing result: %s", data)
				}
			}
			wantBody.Store(string(encoded))
			if calls.Load() != int32(len(valid)) {
				t.Fatalf("unexpected mutation count: %d", calls.Load())
			}
			invalid := append([]map[string]any{{}, changed(tc.args, "unknown_field", true)}, tc.invalid...)
			for key := range tc.args {
				if key == "upstream_id" {
					continue
				}
				invalid = append(invalid, changed(tc.args, key, nil))
				missing := changed(tc.args, key, nil)
				delete(missing, key)
				invalid = append(invalid, missing)
			}
			for _, args := range invalid {
				before := calls.Load()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
				if err == nil && !result.IsError {
					t.Errorf("invalid input accepted: %#v", args)
				}
				if calls.Load() != before {
					t.Errorf("invalid input reached backend: %#v", args)
				}
			}
			for _, code := range []int{400, 401, 403, 404, 409, 500, 503} {
				status.Store(int32(code))
				before := calls.Load()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil || !result.IsError {
					t.Fatalf("HTTP %d must be tool error: %#v %v", code, result, err)
				}
				message := result.Content[0].(*mcp.TextContent).Text
				if !strings.Contains(message, fmt.Sprintf("HTTP %d", code)) || strings.Contains(message, "fixture-key") || strings.Contains(message, "sensitive") {
					t.Errorf("unsafe error: %s", message)
				}
				if calls.Load() != before+1 {
					t.Error("mutation retried")
				}
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stdio diagnostics: %s", stderr.String())
			}
		})
	}
}

func TestCreateProxy(t *testing.T) {
	args := configuration()
	runMutationContract(t, mutationCase{"create_proxy", "POST", "/proxy/create", args, []map[string]any{
		changed(args, "domain_names", []any{}), changed(args, "domain_names", []any{" "}), changed(args, "forward_host", " "),
		changed(args, "forward_scheme", "ftp"), changed(args, "forward_port", 0), changed(args, "forward_port", 65536),
		changed(args, "certificate_id", -1), changed(args, "access_list_id", true), changed(args, "access_list_id", "-1"),
		changed(args, "locations", []any{map[string]any{"path": "bad", "forward_scheme": "http", "forward_host": "host", "forward_port": 80}}),
		changed(args, "locations", []any{map[string]any{"path": "/ok", "forward_scheme": "http", "forward_host": "host", "forward_port": 0}}),
		changed(args, "locations", []any{map[string]any{"path": "/ok", "forward_scheme": "http", "forward_host": "host", "forward_port": 80, "unknown": true}}),
	}})
}

func TestUpdateProxy(t *testing.T) {
	args := changed(configuration(), "id", 42)
	runMutationContract(t, mutationCase{"update_proxy", "PUT", "/proxy/edit", args, []map[string]any{
		changed(args, "id", 0), changed(args, "id", -1), changed(args, "domain_names", []any{}), changed(args, "forward_port", 0), changed(args, "ssl_forced", "false"),
	}})
}

func TestEnableProxy(t *testing.T) {
	args := map[string]any{"id": 42}
	runMutationContract(t, mutationCase{"enable_proxy", "POST", "/proxy/enable", args, []map[string]any{
		changed(args, "id", 0), changed(args, "id", -1), changed(args, "id", 1.5), changed(args, "id", "42"),
	}})
}

func TestDisableProxy(t *testing.T) {
	args := map[string]any{"id": 42}
	runMutationContract(t, mutationCase{"disable_proxy", "POST", "/proxy/disable", args, []map[string]any{
		changed(args, "id", 0), changed(args, "id", -1), changed(args, "id", 1.5), changed(args, "id", "42"),
	}})
}

func TestSetupFrontendProxy(t *testing.T) {
	args := map[string]any{"domain": "frontend.example.test", "id": 0}
	runMutationContract(t, mutationCase{"setup_frontend_proxy", "POST", "/proxy/setupFrontEnd", args, []map[string]any{
		changed(args, "domain", " "), changed(args, "id", -1), changed(args, "id", 1.5), changed(args, "id", "0"),
	}})
}

func TestSetupSecureFrontendProxy(t *testing.T) {
	args := map[string]any{"publicDomain": "public.example.test", "publicCertificateId": 0, "privateDomain": "private.example.test", "privateCertificateId": 8}
	runMutationContract(t, mutationCase{"setup_secure_frontend_proxy", "POST", "/proxy/setupFrontEndSecure", args, []map[string]any{
		changed(args, "publicDomain", " "), changed(args, "privateDomain", ""), changed(args, "publicCertificateId", -1), changed(args, "privateCertificateId", -1),
		changed(args, "privateCertificateId", "8"),
	}})
}
