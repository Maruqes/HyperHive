package main

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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise the real executable over stdin/stdout, including SDK negotiation,
// enumeration, schemas, successful calls, backend errors and clean shutdown.
func TestStdioContract(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "hyperhive-mcp")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	var status atomic.Int32
	status.Store(http.StatusOK)
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/proxy/list" || r.Header.Get("Authorization") != "Bearer integration-token" {
			t.Error("MCP invocation did not honor HTTP contract")
		}
		w.WriteHeader(int(status.Load()))
		if status.Load() != http.StatusOK {
			fmt.Fprint(w, `{"error":"integration-token"}`)
			return
		}
		fmt.Fprint(w, `[{"id":42,"domain_names":["example.test"],"enabled":true,"access_list_id":0}]`)
	}))
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve")
	cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+api.URL+"/api", "MCP_TOKEN=integration-token")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "contract-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer session.Close()
	if session.InitializeResult().ServerInfo.Name != "hyperhive-mcp" {
		t.Fatal("incorrect server identity")
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "list_proxies" {
		t.Fatalf("want exactly list_proxies, got %#v, %v", list, err)
	}
	tool := list.Tools[0]
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.InputSchema == nil || tool.OutputSchema == nil {
		t.Fatal("missing schemas or read-only annotation")
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("call: %#v, %v", result, err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(data, &output); err != nil || len(output.Proxies) != 1 || output.Proxies[0]["id"] != float64(42) || output.Proxies[0]["access_list_id"] != float64(0) {
		t.Fatalf("unexpected structured output: %s, %v", data, err)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "example.test") {
		t.Fatal("missing text fallback")
	}
	before := requests.Load()
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies", Arguments: map[string]any{"token": "untrusted"}})
	if err == nil && !result.IsError {
		t.Fatal("unexpected tool arguments accepted")
	}
	if requests.Load() != before {
		t.Fatal("invalid arguments reached the API")
	}
	status.Store(http.StatusUnauthorized)
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_proxies"})
	if err != nil || !result.IsError {
		t.Fatalf("backend failure must be an MCP tool error: %#v, %v", result, err)
	}
	message := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(message, "HTTP 401") || strings.Contains(message, "integration-token") {
		t.Fatalf("unsafe or unhelpful tool error: %s", message)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
	}
}

func TestCLIValidation(t *testing.T) {
	var output bytes.Buffer
	emptyEnv := func(string) string { return "" }
	if err := run(context.Background(), []string{"--help"}, emptyEnv, &output); err != nil || !strings.Contains(output.String(), "MCP_TOKEN") {
		t.Fatalf("help: %v, %s", err, output.String())
	}
	for _, args := range [][]string{nil, {"serve"}, {"unknown"}, {"--unknown"}} {
		if err := run(context.Background(), args, emptyEnv, &output); err == nil {
			t.Fatalf("missing config or invalid args accepted: %v", args)
		}
	}
}
