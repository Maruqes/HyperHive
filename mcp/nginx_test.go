package hyperhivemcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type nginxCase struct {
	Name, Method, Path, Kind string
	Args                     map[string]any
}

var nginxBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hyperhive-nginx-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	nginxBinary = filepath.Join(dir, "hyperhive-mcp")
	output, err := exec.Command("go", "build", "-o", nginxBinary, "./cmd/hyperhive-mcp").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stdio build: %v\n%s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestNginxBatchContracts(t *testing.T) {
	for _, tc := range nginxCases {
		t.Run(tc.Name, func(t *testing.T) { runNginxContract(t, tc) })
	}
}
func runNginxContract(t *testing.T, tc nginxCase) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var mu sync.Mutex
			calls, status := 0, 200
			body := `[{"id":42,"enabled":false,"certificate_id":0,"domain_names":["fixture.test"],"meta":{"nested":{"flag":false}},"items":[],"clients":[],"future_field":true,"future_counter":9007199254740993}]`
			if tc.Name == "list_dns_providers" {
				body = `[{"id":"fixture-dns","name":"Fixture DNS","credentials":"token = <TOKEN>","future_field":true,"future_counter":9007199254740993}]`
			}
			if tc.Kind == "mutation" {
				body = ""
			}
			if tc.Kind == "download" {
				body = "PK\x00\xff\x01fixture-key"
			}
			response := body
			expected := tc.Args
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if r.Method != tc.Method || r.URL.Path != "/prefix/api"+tc.Path || r.URL.RawQuery != "" {
					t.Errorf("wire got %s %s want %s %s", r.Method, r.URL, tc.Method, tc.Path)
				}
				if r.Header.Get("Authorization") != "Bearer nginx-fixture-key" {
					t.Error("wrong API token")
				}
				if tc.Method == "GET" {
					data, _ := io.ReadAll(r.Body)
					if len(data) != 0 {
						t.Error("list sent body")
					}
				} else {
					var got any
					json.NewDecoder(r.Body).Decode(&got)
					if !reflect.DeepEqual(got, expected) || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("body got %#v want %#v", got, expected)
					}
				}
				if tc.Kind == "download" {
					w.Header().Set("Content-Type", "application/zip")
					w.Header().Set("Content-Disposition", `attachment; filename="certificate-42"`)
				}
				w.WriteHeader(status)
				fmt.Fprint(w, response)
			}))
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			c := mcp.NewClient(&mcp.Implementation{Name: "nginx-contract"}, nil)
			var session *mcp.ClientSession
			var err error
			var stderr bytes.Buffer
			if transport == "http" {
				endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "nginx-fixture-key"}))
				defer endpoint.Close()
				session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"nginx-fixture-key"}}}, nil)
			} else {
				cmd := exec.CommandContext(ctx, nginxBinary, "serve")
				cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=nginx-fixture-key")
				cmd.Stderr = &stderr
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
			var found *mcp.Tool
			for _, tool := range listed.Tools {
				if tool.Name == tc.Name {
					found = tool
				}
			}
			if found == nil {
				t.Fatalf("tool %s not registered", tc.Name)
			}
			read := tc.Kind != "mutation"
			endpoint, _ := found.Meta["hyperhive/endpoint"].(map[string]any)
			if found.InputSchema == nil || found.OutputSchema == nil || found.Annotations == nil || found.Annotations.ReadOnlyHint != read || found.Annotations.DestructiveHint == nil || *found.Annotations.DestructiveHint == read || found.Annotations.IdempotentHint != read || endpoint["method"] != tc.Method || endpoint["path"] != tc.Path {
				t.Fatalf("incorrect discovery: %#v", found)
			}
			call := func(args map[string]any) *mcp.CallToolResult {
				t.Helper()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: args})
				if e != nil {
					t.Fatal(e)
				}
				return result
			}
			assertSuccess := func(args map[string]any, want string) {
				t.Helper()
				result := call(args)
				if result.IsError {
					t.Fatalf("success failed: %#v", result)
				}
				data, _ := json.Marshal(result.StructuredContent)
				var got, expected any
				json.Unmarshal(data, &got)
				json.Unmarshal([]byte(want), &expected)
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("output got %s want %s", data, want)
				}
				if len(result.Content) != 1 {
					t.Fatal("missing fallback")
				}
				if strings.Contains(want, `"future_counter":9007199254740993`) && !strings.Contains(result.Content[0].(*mcp.TextContent).Text, `"future_counter":9007199254740993`) {
					t.Fatal("NGINX list integer precision was lost")
				}
				var text any
				if json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &text) != nil || !reflect.DeepEqual(text, got) {
					t.Fatal("fallback differs")
				}
			}
			want := `{"success":true}`
			if tc.Kind == "list" {
				want = `{"items":` + body + `}`
			}
			if tc.Kind == "download" {
				want = `{"data_base64":"` + base64.StdEncoding.EncodeToString([]byte(body)) + `","content_type":"application/zip","content_disposition":"attachment; filename=\"certificate-42\""}`
			}
			assertSuccess(tc.Args, want)
			// All required replacement fields must survive omission/false/zero checks.
			invalid := []map[string]any{changed(tc.Args, "unknown_field", true)}
			for key := range tc.Args {
				if key == "upstream_id" {
					continue
				}
				invalid = append(invalid, changed(tc.Args, key, nil))
				missing := changed(tc.Args, key, nil)
				delete(missing, key)
				invalid = append(invalid, missing)
			}
			if _, ok := tc.Args["id"]; ok {
				invalid = append(invalid, changed(tc.Args, "id", 0), changed(tc.Args, "id", -1), changed(tc.Args, "id", "42"))
			}
			if _, ok := tc.Args["domain_names"]; ok {
				invalid = append(invalid, changed(tc.Args, "domain_names", []any{}), changed(tc.Args, "domain_names", []any{" "}), changed(tc.Args, "enabled", false), changed(tc.Args, "meta", map[string]any{"unknown": true}))
			}
			if _, ok := tc.Args["certificate_id"]; ok {
				invalid = append(invalid, changed(tc.Args, "certificate_id", -1))
			}
			if _, ok := tc.Args["incoming_port"]; ok {
				invalid = append(invalid, changed(tc.Args, "incoming_port", 0), changed(tc.Args, "forwarding_port", 65536), changed(tc.Args, "forwarding_host", func() string {
					if tc.Args["upstream_id"] != nil {
						return "conflicting.test"
					}
					return ""
				}()), changed(tc.Args, "enabled", false))
			}
			if _, ok := tc.Args["forward_http_code"]; ok {
				invalid = append(invalid, changed(tc.Args, "forward_http_code", true), changed(tc.Args, "forward_http_code", 301.5), changed(tc.Args, "forward_scheme", "ftp"), changed(tc.Args, "forward_domain_name", ""))
			}
			if tc.Name == "create_access_list" {
				invalid = append(invalid, changed(tc.Args, "name", ""), changed(tc.Args, "items", []any{map[string]any{"username": "user", "password": "private", "id": 1}}), changed(tc.Args, "clients", []any{map[string]any{"directive": "unknown", "address": "all"}}))
			}
			for _, args := range invalid {
				mu.Lock()
				before := calls
				mu.Unlock()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: args})
				if e == nil && !result.IsError {
					t.Errorf("invalid args accepted: %#v", args)
				}
				mu.Lock()
				after := calls
				mu.Unlock()
				if before != after {
					t.Errorf("invalid args reached API: %#v", args)
				}
			}
			// Omitted-vs-false/zero and numeric/string redirect status are independent wire checks.
			if tc.Kind == "mutation" && len(tc.Args) > 1 {
				variant := changed(tc.Args, "unknown", true)
				delete(variant, "unknown")
				for key, value := range variant {
					if flag, ok := value.(bool); ok {
						variant[key] = !flag
					}
				}
				if _, ok := variant["forward_http_code"]; ok {
					variant["forward_http_code"] = float64(302)
					variant["forward_scheme"] = "$scheme"
				}
				if tc.Name == "create_access_list" {
					variant["items"] = []any{}
					variant["clients"] = []any{}
				}
				mu.Lock()
				expected = variant
				mu.Unlock()
				assertSuccess(variant, `{"success":true}`)
				mu.Lock()
				expected = tc.Args
				mu.Unlock()
			}
			for _, code := range []int{400, 401, 403, 404, 409, 500, 503} {
				mu.Lock()
				status = code
				response = "nginx-fixture-key private response"
				before := calls
				mu.Unlock()
				result := call(tc.Args)
				if !result.IsError {
					t.Fatalf("HTTP %d accepted", code)
				}
				text := result.Content[0].(*mcp.TextContent).Text
				if !strings.Contains(text, fmt.Sprintf("HTTP %d", code)) || strings.Contains(text, "nginx-fixture-key") || strings.Contains(text, "private response") {
					t.Fatalf("unsafe error: %s", text)
				}
				mu.Lock()
				after := calls
				mu.Unlock()
				if after != before+1 {
					t.Fatal("write retried")
				}
			}
			if tc.Kind == "list" {
				for _, bad := range []string{"", "{", "{}", "[null]", "[1]", "[] {}"} {
					mu.Lock()
					status = 200
					response = bad
					mu.Unlock()
					if !call(tc.Args).IsError {
						t.Errorf("invalid list response accepted: %q", bad)
					}
				}
				for _, empty := range []string{"null", "[]"} {
					mu.Lock()
					status = 200
					response = empty
					mu.Unlock()
					assertSuccess(tc.Args, `{"items":[]}`)
				}
			}
			if tc.Kind == "download" {
				mu.Lock()
				status = 200
				response = ""
				mu.Unlock()
				assertSuccess(tc.Args, `{"data_base64":"","content_type":"application/zip","content_disposition":"attachment; filename=\"certificate-42\""}`)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
		})
	}
}
