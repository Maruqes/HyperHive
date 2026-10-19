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

// Capture the raw HTTP response before the SDK decodes arbitrary objects to float64.
type dnsCaptureTransport struct {
	mu   sync.Mutex
	body []byte
}

func (tr *dnsCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := (bearerTransport{"dns-fixture-key"}).RoundTrip(req)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	tr.mu.Lock()
	tr.body = append([]byte(nil), data...)
	tr.mu.Unlock()
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

type dnsCase struct {
	name, method, path, body string
	args                     map[string]any
}

func TestDNSContracts(t *testing.T) {
	cases := []dnsCase{
		{"dns_alias_add", "POST", "/dnsmasq/alias/add", "", map[string]any{"alias": "fixture.test", "ip": "192.0.2.1"}},
		{"dns_alias_get", "GET", "/dnsmasq/alias/get", `{"exists":true,"counter":9007199254740993}`, map[string]any{"alias": "fixture+?&.test", "ip": "2001:db8::1"}},
		{"dns_aliases_list", "GET", "/dnsmasq/alias/all", `{"aliases":[{"alias":"fixture.test","ip":"2001:db8::1","counter":9007199254740993}],"counter":9007199254740993}`, map[string]any{}},
	}
	for _, tc := range cases {
		for _, transport := range []string{"http", "stdio"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) { runDNSContract(t, tc, transport) })
		}
	}
}
func runDNSContract(t *testing.T, tc dnsCase, transport string) {
	var mu sync.Mutex
	calls, status := 0, 200
	response := tc.body
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if r.Method != tc.method || r.URL.Path != "/prefix/api"+tc.path {
			t.Errorf("wire: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer dns-fixture-key" || r.Header.Get("Accept") != "application/json" {
			t.Error("wrong API authentication/accept")
		}
		data, _ := io.ReadAll(r.Body)
		switch tc.method {
		case "GET":
			if len(data) != 0 {
				t.Error("GET body")
			}
			if len(tc.args) == 0 {
				if r.URL.RawQuery != "" {
					t.Error("unexpected query")
				}
			} else {
				if r.URL.Query().Get("alias") != strings.TrimSpace(tc.args["alias"].(string)) || r.URL.Query().Get("ip") != strings.TrimSpace(tc.args["ip"].(string)) || len(r.URL.Query()) != 2 {
					t.Errorf("query: %s", r.URL.RawQuery)
				}
			}
		case "POST":
			var got map[string]any
			json.Unmarshal(data, &got)
			if !reflect.DeepEqual(got, tc.args) || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
				t.Errorf("body/query: %s %s", data, r.URL.RawQuery)
			}
		}
		w.WriteHeader(status)
		fmt.Fprint(w, response)
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "dns-contract"}, nil)
	var session *mcp.ClientSession
	var err error
	var stderr bytes.Buffer
	capture := &dnsCaptureTransport{}
	if transport == "http" {
		endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/prefix/api", MCPToken: "dns-fixture-key"}))
		defer endpoint.Close()
		session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: capture}}, nil)
	} else {
		cmd := exec.CommandContext(ctx, nginxBinary, "serve")
		cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend.URL+"/prefix/api", "MCP_TOKEN=dns-fixture-key")
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
	var tool *mcp.Tool
	for _, item := range listed.Tools {
		if item.Name == tc.name {
			tool = item
		}
		if strings.HasPrefix(item.Name, "dns_") && (strings.Contains(item.Name, "remove") || strings.Contains(item.Name, "delete")) {
			t.Fatal("deletion tool exposed")
		}
	}
	if tool == nil {
		t.Fatalf("%s not registered", tc.name)
	}
	read := tc.method == "GET"
	endpoint, _ := tool.Meta["hyperhive/endpoint"].(map[string]any)
	if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint != read || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint == read || tool.Annotations.IdempotentHint != read || endpoint["method"] != tc.method || endpoint["path"] != tc.path {
		t.Fatalf("discovery: %#v", tool)
	}
	call := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	success := func(want string) {
		t.Helper()
		out := call(tc.args)
		if out.IsError {
			t.Fatalf("success: %+v", out)
		}
		if len(out.Content) != 1 {
			t.Fatal("missing fallback")
		}
		text := out.Content[0].(*mcp.TextContent).Text
		var got, expected any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		dec.Decode(&got)
		dec = json.NewDecoder(strings.NewReader(want))
		dec.UseNumber()
		dec.Decode(&expected)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("response: %s want %s", text, want)
		}
		if strings.Contains(want, "9007199254740993") && !strings.Contains(text, "9007199254740993") {
			t.Fatalf("integer lost: %s", text)
		}
	}
	want := tc.body
	if tc.method == "POST" {
		want = `{"success":true}`
	}
	success(want)
	if transport == "http" && strings.Contains(want, "9007199254740993") {
		capture.mu.Lock()
		wire := append([]byte(nil), capture.body...)
		capture.mu.Unlock()
		if !bytes.Contains(wire, []byte(`"structuredContent"`)) || !bytes.Contains(wire, []byte(`:9007199254740993`)) {
			t.Fatalf("structured wire precision lost: %s", wire)
		}
	}
	// Raw JSON checks cover root null/array and field type shapes beyond map arguments.
	for _, raw := range []string{"null", "[]", `{"alias":null,"ip":null}`} {
		mu.Lock()
		before := calls
		mu.Unlock()
		out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: json.RawMessage(raw)})
		if err == nil && !out.IsError {
			t.Fatalf("raw invalid accepted: %s", raw)
		}
		mu.Lock()
		after := calls
		mu.Unlock()
		if after != before {
			t.Fatalf("raw invalid reached backend: %s", raw)
		}
	}

	invalid := []map[string]any{{"unknown": true}}
	for key := range tc.args {
		bad := map[string]any{}
		for k, v := range tc.args {
			bad[k] = v
		}
		delete(bad, key)
		invalid = append(invalid, bad)
		for _, value := range []any{nil, false, 0, "", " ", "bad value", "bad,value", "bad\nvalue", []any{}, map[string]any{}} {
			bad = map[string]any{}
			for k, v := range tc.args {
				bad[k] = v
			}
			bad[key] = value
			invalid = append(invalid, bad)
		}
	}
	if len(tc.args) == 0 {
		invalid = append(invalid, map[string]any{"unknown": nil})
	}
	for _, args := range invalid {
		mu.Lock()
		before := calls
		mu.Unlock()
		out := call(args)
		if !out.IsError {
			t.Fatalf("invalid accepted: %#v", args)
		}
		mu.Lock()
		after := calls
		mu.Unlock()
		if after != before {
			t.Fatal("invalid reached backend")
		}
	}
	if tc.name == "dns_aliases_list" {
		mu.Lock()
		response = `{"aliases":[]}`
		mu.Unlock()
		success(`{"aliases":[]}`)
	}
	if tc.name == "dns_alias_get" {
		mu.Lock()
		response = `{"exists":false}`
		mu.Unlock()
		success(`{"exists":false}`)
	}
	for _, code := range []int{400, 401, 403, 404, 409, 500} {
		mu.Lock()
		status = code
		response = "dns-fixture-key secret-backend-body"
		before := calls
		mu.Unlock()
		out := call(tc.args)
		if !out.IsError {
			t.Fatalf("HTTP %d accepted", code)
		}
		raw, _ := json.Marshal(out)
		if bytes.Contains(raw, []byte("secret-backend-body")) || bytes.Contains(raw, []byte("dns-fixture-key")) {
			t.Fatal("sensitive body leak")
		}
		mu.Lock()
		after := calls
		mu.Unlock()
		if after != before+1 {
			t.Fatalf("retry count: %d", after-before)
		}
	}

	if len(tc.args) > 0 {
		// GET normalizes lookup whitespace; POST must reject it before writing
		// original values into the backend's unescaped configuration.
		tc.args = map[string]any{"alias": " fixture.test ", "ip": " 192.0.2.1 "}
		mu.Lock()
		status = 200
		response = tc.body
		mu.Unlock()
		if tc.method == "GET" {
			success(want)
		} else {
			mu.Lock()
			before := calls
			mu.Unlock()
			if !call(tc.args).IsError {
				t.Fatal("write accepted surrounding whitespace")
			}
			mu.Lock()
			after := calls
			mu.Unlock()
			if after != before {
				t.Fatal("invalid DNS write reached backend")
			}
		}
	}
	if tc.method == "GET" {
		for _, bad := range []string{"null", "[]", "{}", `{"aliases":[null]}`, `{"aliases":[{"alias":"fixture.test"}]}`, `{"aliases":null}`, `{"aliases":[{"alias":42,"ip":"192.0.2.1"}]}`, `{"exists":null}`, `{"exists":"false"}`, ""} {
			mu.Lock()
			status = 200
			response = bad
			mu.Unlock()
			if !call(tc.args).IsError {
				t.Fatalf("malformed response accepted: %s", bad)
			}
		}
	}
}
