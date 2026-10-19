package hyperhivemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type spaCase struct {
	name, path, body string
	args             map[string]any
}

var spaCases = []spaCase{{"list_spa_ports", "/spa/", `{"spa_ports":[{"port":443,"description":"","created_at":"2026-10-19T00:00:00Z"}],"counter":9007199254740993,"flag":false}`, map[string]any{}}}

func init() {
	spaCases = append(spaCases, spaCase{"list_spa_allows", "/spa/allow/443", `{"port":443,"allows":[{"ip":"192.0.2.1","remaining_seconds":0}],"counter":9007199254740993,"flag":false}`, map[string]any{"port": 443}})
}

func TestSPAContracts(t *testing.T) {
	for _, tc := range spaCases {
		for _, transport := range []string{"http", "stdio"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				var mu sync.Mutex
				calls, status := 0, 200
				body := tc.body
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					data, _ := io.ReadAll(r.Body)
					if r.Method != "GET" || r.URL.Path != "/prefix/api"+tc.path || r.URL.RawQuery != "" || len(data) != 0 || r.Header.Get("Authorization") != "Bearer spa-fixture-key" {
						t.Errorf("wrong request: %s %s", r.Method, r.URL)
					}
					w.WriteHeader(status)
					fmt.Fprint(w, body)
				}))
				defer backend.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				c := mcp.NewClient(&mcp.Implementation{Name: "spa-contract"}, nil)
				var session *mcp.ClientSession
				var err error
				var stderr bytes.Buffer
				if transport == "http" {
					endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "spa-fixture-key"}))
					defer endpoint.Close()
					session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"spa-fixture-key"}}}, nil)
				} else {
					cmd := exec.CommandContext(ctx, nginxBinary, "serve")
					cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=spa-fixture-key")
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
				found := false
				for _, tool := range listed.Tools {
					if tool.Name == "create_spa_port" || tool.Name == "delete_spa_port" || tool.Name == "allow_spa_ip" {
						t.Fatalf("excluded tool registered: %s", tool.Name)
					}
					if tool.Name == tc.name {
						found = true
						ep, _ := tool.Meta["hyperhive/endpoint"].(map[string]any)
						if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint || ep["method"] != "GET" || ep["path"] != strings.ReplaceAll(tc.path, "443", "{port}") {
							t.Fatalf("incorrect discovery: %#v", tool)
						}
					}
				}
				if !found {
					t.Fatalf("%s not registered", tc.name)
				}
				call := func(args map[string]any) *mcp.CallToolResult {
					t.Helper()
					r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
					if e != nil {
						t.Fatal(e)
					}
					return r
				}
				result := call(tc.args)
				if result.IsError {
					t.Fatalf("success failed: %#v", result)
				}
				if len(result.Content) != 1 {
					t.Fatal("missing text fallback")
				}
				text := result.Content[0].(*mcp.TextContent).Text
				if len(tc.args) > 0 && !strings.Contains(text, `"remaining_seconds":0`) {
					t.Fatal("zero timeout was lost")
				}
				if !strings.Contains(text, `9007199254740993`) || !strings.Contains(text, `"flag":false`) {
					t.Fatalf("response changed: %s", text)
				}
				var expected, actual any
				json.Unmarshal([]byte(`{"result":`+tc.body+`}`), &expected)
				encoded, _ := json.Marshal(result.StructuredContent)
				json.Unmarshal(encoded, &actual)
				expectedJSON, _ := json.Marshal(expected)
				actualJSON, _ := json.Marshal(actual)
				if string(expectedJSON) != string(actualJSON) {
					t.Fatalf("response fields lost: %s", encoded)
				}
				invalid := []map[string]any{{"unknown": true}}
				if len(tc.args) > 0 {
					for _, value := range []any{nil, 0, -1, 65536, 443.5, "443", false} {
						invalid = append(invalid, map[string]any{"port": value})
					}
					invalid = append(invalid, map[string]any{})
				}
				for _, args := range invalid {
					mu.Lock()
					before := calls
					mu.Unlock()
					r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
					if e == nil && !r.IsError {
						t.Errorf("invalid arguments accepted: %#v", args)
					}
					mu.Lock()
					if calls != before {
						t.Error("invalid arguments reached backend")
					}
					mu.Unlock()
				}
				empty := `{"spa_ports":[]}`
				if len(tc.args) > 0 {
					empty = `{"port":443,"allows":[]}`
				}
				mu.Lock()
				body = empty
				mu.Unlock()
				if call(tc.args).IsError {
					t.Fatal("empty array rejected")
				}
				malformed := []string{`null`, `{}`, `{"spa_ports":null,"allows":null}`, `{"spa_ports":[],"allows":[],"password_hash":"private-hash"}`, `{"spa_ports":[],"allows":[],"nested":{"PasswordHash":"private-hash"}}`, `not-json`}
				if len(tc.args) == 0 {
					malformed = append(malformed, `{"spa_ports":[null]}`, `{"spa_ports":[{"port":443,"description":null,"created_at":"now"}]}`, `{"spa_ports":[{"port":65536,"description":"","created_at":"now"}]}`, `{"spa_ports":[{"port":443,"description":"","created_at":false}]}`, `{"spa_ports":[{"port":443,"description":"","created_at":"now","password":"private-hash"}]}`)
				} else {
					malformed = append(malformed, `{"port":444,"allows":[]}`, `{"port":"443","allows":[]}`, `{"allows":[]}`, `{"port":443,"allows":[null]}`, `{"port":443,"allows":[{"ip":"192.0.2.1","remaining_seconds":null}]}`, `{"port":443,"allows":[{"ip":"192.0.2.1","remaining_seconds":1.5}]}`, `{"port":443,"allows":[{"ip":false,"remaining_seconds":0}]}`)
				}
				for _, bad := range malformed {
					mu.Lock()
					body = bad
					mu.Unlock()
					r := call(tc.args)
					if !r.IsError || strings.Contains(r.Content[0].(*mcp.TextContent).Text, "private-hash") {
						t.Fatalf("unsafe malformed response: %#v", r)
					}
				}
				for _, code := range []int{400, 401, 403, 404, 409, 500, 503} {
					mu.Lock()
					status = code
					body = "spa-fixture-key private password"
					before := calls
					mu.Unlock()
					r := call(tc.args)
					if !r.IsError {
						t.Fatalf("HTTP %d accepted", code)
					}
					txt := r.Content[0].(*mcp.TextContent).Text
					if strings.Contains(txt, "spa-fixture-key") || strings.Contains(txt, "private") {
						t.Fatal("error body leaked")
					}
					mu.Lock()
					if calls != before+1 {
						t.Error("request retried")
					}
					mu.Unlock()
				}
				// CommandTransport.Close waits for exec.Cmd.Wait, including the
				// stderr copy goroutine, before inspecting the captured bytes.
				// The deferred Close remains for early failure cleanup.
				if err := session.Close(); err != nil {
					t.Fatalf("close SPA session before stderr inspection: %v", err)
				}
				if strings.Contains(stderr.String(), "private-hash") || strings.Contains(stderr.String(), "spa-fixture-key") {
					t.Fatal("secret leaked on stderr")
				}
			})
		}
	}
}
