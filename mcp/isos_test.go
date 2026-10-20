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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type isosCase struct {
	name, method, path string
	args               map[string]any
}

var isosCases = []isosCase{
	{"list_isos", "GET", "/isos/", map[string]any{}},
	{"start_iso_download", "POST", "/isos/download", map[string]any{"url": "https://fixture.test/image.iso", "iso_name": " image.iso", "nfs_share_id": 42}},
}

func TestISOsContracts(t *testing.T) {
	for _, tc := range isosCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"http", "stdio"} {
				t.Run(transport, func(t *testing.T) {
					var mu sync.Mutex
					calls, status := 0, 200
					response := `{"Id":9007199254740993,"MachineName":"fixture","FilePath":"/isos/image.iso","Name":"image.iso","available_on_slaves":{"online":true,"offline":false},"future_counter":9007199254740993}`
					if tc.name == "list_isos" {
						response = "[" + response + "]"
					}
					goodResponse := response
					if tc.method == "POST" {
						status = 202
						response = `{"status":"started","future_counter":9007199254740993}`
						goodResponse = response
					}
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						calls++
						if r.Method != tc.method || r.URL.Path != "/prefix/api"+tc.path || r.URL.RawQuery != "" {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						}
						if r.Header.Get("Authorization") != "Bearer isos-fixture-key" || r.Header.Get("Accept") != "application/json" {
							t.Error("missing API auth/accept")
						}
						data, _ := io.ReadAll(r.Body)
						if tc.method == "GET" {
							if len(data) != 0 {
								t.Error("GET sent a body")
							}
						} else {
							expectedData, _ := json.Marshal(tc.args)
							var expected any
							json.Unmarshal(expectedData, &expected)
							var got any
							json.Unmarshal(data, &got)
							if !reflect.DeepEqual(got, expected) || r.Header.Get("Content-Type") != "application/json" {
								t.Errorf("unexpected body %s", data)
							}
						}
						w.WriteHeader(status)
						fmt.Fprint(w, response)
					}))
					defer backend.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					c := mcp.NewClient(&mcp.Implementation{Name: "isos-contract"}, nil)
					var session *mcp.ClientSession
					var err error
					var stderr bytes.Buffer
					if transport == "http" {
						endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "isos-fixture-key"}))
						defer endpoint.Close()
						session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"isos-fixture-key"}}}, nil)
					} else {
						cmd := exec.CommandContext(ctx, nginxBinary, "serve")
						cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=isos-fixture-key")
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
						if tool.Name == tc.name {
							found = tool
						}
						if tool.Name != "remove_vm_isos" && strings.Contains(tool.Name, "iso") && (strings.Contains(tool.Name, "delete") || strings.Contains(tool.Name, "remove")) {
							t.Fatal("deletion tool exposed")
						}
					}
					if found == nil {
						t.Fatalf("tool %s not registered", tc.name)
					}
					endpoint, _ := found.Meta["hyperhive/endpoint"].(map[string]any)
					path := tc.path
					if found.InputSchema == nil || found.OutputSchema == nil || found.Annotations == nil || found.Annotations.ReadOnlyHint != (tc.method == "GET") || found.Annotations.DestructiveHint == nil || *found.Annotations.DestructiveHint || found.Annotations.IdempotentHint != (tc.method != "POST") || endpoint["method"] != tc.method || endpoint["path"] != path {
						t.Fatalf("wrong discovery %#v", found)
					}
					call := func(args map[string]any) (*mcp.CallToolResult, error) {
						return session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
					}
					for _, raw := range []json.RawMessage{json.RawMessage("null"), json.RawMessage("[]")} {
						mu.Lock()
						before := calls
						mu.Unlock()
						result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: raw})
						if err == nil && !result.IsError {
							t.Fatalf("accepted root arguments %s", raw)
						}
						mu.Lock()
						after := calls
						mu.Unlock()
						if before != after {
							t.Fatal("invalid root arguments reached backend")
						}
					}
					if tc.method == "GET" {
						result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name})
						if err != nil || result.IsError {
							t.Fatalf("omitted list arguments failed: %v %#v", err, result)
						}
					}
					result, err := call(tc.args)
					if err != nil || result.IsError {
						t.Fatalf("success failed %v %#v", err, result)
					}
					text := result.Content[0].(*mcp.TextContent).Text
					if !strings.Contains(text, "9007199254740993") || strings.Contains(text, "9007199254740992") {
						t.Fatalf("integer precision lost: %s", text)
					}
					var output map[string]json.RawMessage
					json.Unmarshal([]byte(text), &output)
					if !bytes.Equal(bytes.TrimSpace(output["result"]), []byte(goodResponse)) {
						var a, b any
						json.Unmarshal(output["result"], &a)
						json.Unmarshal([]byte(goodResponse), &b)
						if !reflect.DeepEqual(a, b) {
							t.Fatalf("response changed: %s", text)
						}
					}
					invalid := []map[string]any{changed(tc.args, "unknown", true)}
					for key := range tc.args {
						missing := changed(tc.args, key, nil)
						delete(missing, key)
						invalid = append(invalid, missing, changed(tc.args, key, nil), changed(tc.args, key, false))
						if key == "nfs_share_id" {
							invalid = append(invalid, changed(tc.args, key, 0), changed(tc.args, key, -1), changed(tc.args, key, 1.5), changed(tc.args, key, "42"))
						} else {
							invalid = append(invalid, changed(tc.args, key, "  \n\t"), changed(tc.args, key, "image.ISO"), changed(tc.args, key, "image.iso?x=1"), changed(tc.args, key, 42))
						}
					}
					if tc.method == "POST" {
						for _, name := range []string{"../outside.iso", "a/b.iso", `a\b.iso`, "bad\x00.iso"} {
							invalid = append(invalid, changed(tc.args, "iso_name", name))
						}
						for _, url := range []string{"file:///image.iso", "image.iso", "https://user:pass@fixture.test/image.iso", "https://fixture.test/image.iso#end.iso", "https:///image.iso"} {
							invalid = append(invalid, changed(tc.args, "url", url))
						}
					}
					for _, args := range invalid {
						mu.Lock()
						before := calls
						mu.Unlock()
						result, err := call(args)
						if err == nil && !result.IsError {
							t.Fatalf("accepted invalid %#v", args)
						}
						mu.Lock()
						after := calls
						mu.Unlock()
						if before != after {
							t.Fatalf("invalid args reached backend %#v", args)
						}
					}
					for _, code := range []int{400, 401, 403, 404, 409, 500} {
						mu.Lock()
						status = code
						response = "sensitive backend secret"
						before := calls
						mu.Unlock()
						result, err := call(tc.args)
						if err != nil || !result.IsError {
							t.Fatalf("HTTP %d accepted: %v %#v", code, err, result)
						}
						data, _ := json.Marshal(result)
						if strings.Contains(string(data), "sensitive backend secret") {
							t.Fatal("backend detail leaked")
						}
						mu.Lock()
						after := calls
						mu.Unlock()
						if after != before+1 {
							t.Fatal("request retried")
						}
					}
					malformed := []string{"null", "{}", "[]", "{", "{} {}", `{"status":false}`, `{"status":"done"}`}
					if tc.name == "list_isos" {
						malformed = []string{"null", "{}", "[null]", "[{}]", "[", "[] []", `[{"Id":1,"MachineName":"fixture","FilePath":"/isos/a.iso","Name":"a.iso","available_on_slaves":{"x":0}}]`}
					}
					for _, body := range malformed {
						mu.Lock()
						status = 200
						if tc.method == "POST" {
							status = 202
						}
						response = body
						mu.Unlock()
						result, err := call(tc.args)
						if err == nil && !result.IsError {
							t.Fatalf("accepted malformed response %s", body)
						}
					}
					if tc.name == "list_isos" {
						mu.Lock()
						status = 200
						response = "[]"
						mu.Unlock()
						result, err := call(tc.args)
						if err != nil || result.IsError {
							t.Fatal("empty list failed")
						}
					}
					if tc.method == "POST" {
						for _, code := range []int{200, 201, 204} {
							mu.Lock()
							status = code
							response = `{"status":"started"}`
							mu.Unlock()
							result, err := call(tc.args)
							if err == nil && !result.IsError {
								t.Fatalf("accepted unexpected initiation HTTP %d", code)
							}
						}
					}
					// CommandTransport.Close closes stdin and waits for cmd.Wait,
					// including its stderr-copy goroutine, before reading the buffer.
					if err := session.Close(); err != nil {
						t.Fatalf("close MCP session: %v", err)
					}
					if stderr.Len() != 0 {
						t.Fatalf("unexpected stderr %s", stderr.String())
					}
				})
			}
		})
	}
}
