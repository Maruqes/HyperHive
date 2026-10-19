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

type notesCase struct {
	name, method, path string
	args               map[string]any
}

var notesCases = []notesCase{
	{"list_notes", "GET", "/notes/", map[string]any{}},
	{"get_note", "GET", "/notes/42", map[string]any{"id": 42}},
	{"create_note", "POST", "/notes/", map[string]any{"titulo": " Title ", "nota": "Body\nsecond line"}},
	{"replace_note", "PUT", "/notes/42", map[string]any{"id": 42, "titulo": " Title ", "nota": "Body\nsecond line"}},
	{"patch_note", "PATCH", "/notes/42", map[string]any{"id": 42, "titulo": " Title ", "nota": "Body\nsecond line"}},
}

func TestNotesContracts(t *testing.T) {
	for _, tc := range notesCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"http", "stdio"} {
				t.Run(transport, func(t *testing.T) {
					var mu sync.Mutex
					calls, status := 0, 200
					response := `{"id":9007199254740993,"titulo":" Title ","nota":"Body\nsecond line","future_counter":9007199254740993}`
					if tc.name == "list_notes" {
						response = "[" + response + "]"
					}
					goodResponse := response
					if tc.method == "POST" {
						status = 201
					}
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						calls++
						if r.Method != tc.method || r.URL.Path != "/prefix/api"+tc.path || r.URL.RawQuery != "" {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						}
						if r.Header.Get("Authorization") != "Bearer notes-fixture-key" || r.Header.Get("Accept") != "application/json" {
							t.Error("missing API auth/accept")
						}
						data, _ := io.ReadAll(r.Body)
						if tc.method == "GET" {
							if len(data) != 0 {
								t.Error("GET sent a body")
							}
						} else {
							expected := map[string]any{"titulo": tc.args["titulo"], "nota": tc.args["nota"]}
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
					c := mcp.NewClient(&mcp.Implementation{Name: "notes-contract"}, nil)
					var session *mcp.ClientSession
					var err error
					var stderr bytes.Buffer
					if transport == "http" {
						endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "notes-fixture-key"}))
						defer endpoint.Close()
						session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"notes-fixture-key"}}}, nil)
					} else {
						cmd := exec.CommandContext(ctx, nginxBinary, "serve")
						cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=notes-fixture-key")
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
						if strings.Contains(tool.Name, "note") && strings.Contains(tool.Name, "delete") {
							t.Fatal("deletion tool exposed")
						}
					}
					if found == nil {
						t.Fatalf("tool %s not registered", tc.name)
					}
					endpoint, _ := found.Meta["hyperhive/endpoint"].(map[string]any)
					path := tc.path
					if _, ok := tc.args["id"]; ok {
						path = "/notes/{id}"
					}
					if found.InputSchema == nil || found.OutputSchema == nil || found.Annotations == nil || found.Annotations.ReadOnlyHint != (tc.method == "GET") || found.Annotations.DestructiveHint == nil || *found.Annotations.DestructiveHint != (tc.method == "PUT" || tc.method == "PATCH") || found.Annotations.IdempotentHint != (tc.method != "POST") || endpoint["method"] != tc.method || endpoint["path"] != path {
						t.Fatalf("wrong discovery %#v", found)
					}
					call := func(args map[string]any) (*mcp.CallToolResult, error) {
						return session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
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

					// Explicit null roots must not be treated as omitted arguments.
					for _, raw := range []string{"null", "[]"} {
						mu.Lock()
						before := calls
						mu.Unlock()
						result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(raw)})
						if err == nil && !result.IsError {
							t.Fatalf("accepted invalid root %s", raw)
						}
						mu.Lock()
						after := calls
						mu.Unlock()
						if after != before {
							t.Fatalf("invalid root %s reached backend", raw)
						}
					}
					if tc.name == "list_notes" {
						result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name})
						if err != nil || result.IsError {
							t.Fatalf("omitted arguments rejected: %v %#v", err, result)
						}
					}
					invalid := []map[string]any{changed(tc.args, "unknown", true)}
					for key := range tc.args {
						missing := changed(tc.args, key, nil)
						delete(missing, key)
						invalid = append(invalid, missing, changed(tc.args, key, nil), changed(tc.args, key, false))
						if key == "id" {
							invalid = append(invalid, changed(tc.args, key, 0), changed(tc.args, key, -1), changed(tc.args, key, 1.5), changed(tc.args, key, "42"))
						} else {
							invalid = append(invalid, changed(tc.args, key, "  \n\t"), changed(tc.args, key, 42))
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
					malformed := []string{"null", "{}", "[]", "{", "{} {}", `{"id":1,"titulo":false,"nota":"x"}`, `{"id":1,"titulo":"x"}`}
					if tc.name == "list_notes" {
						malformed = []string{"null", "{}", "[null]", "[{}]", "[", "[] []"}
					}
					for _, body := range malformed {
						mu.Lock()
						status = 200
						response = body
						mu.Unlock()
						result, err := call(tc.args)
						if err == nil && !result.IsError {
							t.Fatalf("accepted malformed response %s", body)
						}
					}
					if tc.name == "list_notes" {
						mu.Lock()
						status = 200
						response = "[]"
						mu.Unlock()
						result, err := call(tc.args)
						if err != nil || result.IsError {
							t.Fatal("empty list failed")
						}
					}
					// CommandTransport.Close waits for the process and its stderr
					// copy goroutine before the bytes.Buffer is inspected.
					if err := session.Close(); err != nil {
						t.Fatalf("close session: %v", err)
					}
					if stderr.Len() != 0 {
						t.Fatalf("unexpected stderr %s", stderr.String())
					}
				})
			}
		})
	}
}
