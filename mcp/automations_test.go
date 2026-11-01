package hyperhivemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAutomationToolsContract(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var putBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer automation-key" {
			t.Error("missing API auth")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/automations/":
			io.WriteString(w, `{"paused":false,"node":{"available":true,"version":"v22"},"automations":[{"id":"a1","name":"CPU"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/automations/":
			if !strings.Contains(string(body), `"condition":"app('db').cpu \u003e= 95"`) {
				t.Errorf("create body %s", body)
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":"a2","name":"New","enabled":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/automations/evaluate":
			http.Error(w, "invalid condition: unexpected token", http.StatusBadRequest)
		case r.Method == http.MethodGet && r.URL.Path == "/api/automations/a1":
			io.WriteString(w, `{"id":"a1","name":"CPU","description":"d","enabled":false,"condition":"x > 1","cooldown_seconds":5,"for_seconds":0,"timeout_seconds":60,"max_runs_per_hour":3,"state":{}}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/automations/a1":
			_ = json.Unmarshal(body, &putBody)
			io.WriteString(w, `{"id":"a1","enabled":true}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/automations/a1/files/content":
			http.Error(w, "not found", http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/api/automations/a1/files/content":
			io.WriteString(w, `{"path":"lib/x.ts","size":3}`)
		default:
			http.Error(w, "unexpected", http.StatusTeapot)
		}
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reviewDir, backupDir := t.TempDir(), filepath.Join(t.TempDir(), "backups")
	if err := os.Chmod(reviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/api", MCPToken: "automation-key", SafetyReviewDir: reviewDir, VMBackupDir: backupDir}))
	defer endpoint.Close()
	decision := "approve"
	prompts := 0
	opts := &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		if decision == "decline" {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}, nil
	}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "automation-contract"}, opts).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"automation-key"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	risks := map[string]any{}
	for _, tool := range listed.Tools {
		if tool.Meta["hyperhive/area"] == "automations" {
			risks[tool.Name] = tool.Meta["hyperhive/risk"]
			if strings.Contains(tool.Name, "delete") || strings.Contains(tool.Name, "remove") || strings.Contains(tool.Name, "kill") {
				t.Fatalf("deletion-like automation tool registered: %s", tool.Name)
			}
		}
	}
	if len(risks) != 14 || risks["run_automation"] != "very high" || risks["write_automation_file"] != "high" || risks["create_automation"] != "medium" || risks["list_automations"] != "low" {
		t.Fatalf("risks: %v", risks)
	}
	call := func(name string, args map[string]any) (*mcp.CallToolResult, string) {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		text := string(data)
		if result.IsError && len(result.Content) > 0 {
			text = result.Content[0].(*mcp.TextContent).Text
		}
		return result, text
	}
	if result, text := call("list_automations", map[string]any{}); result.IsError || !strings.Contains(text, `"name":"CPU"`) {
		t.Fatalf("list: %s", text)
	}
	if result, text := call("create_automation", map[string]any{"name": "New", "condition": "app('db').cpu >= 95"}); result.IsError || !strings.Contains(text, `"id":"a2"`) {
		t.Fatalf("create: %s", text)
	}
	if result, text := call("evaluate_automation_condition", map[string]any{"condition": "x >"}); !result.IsError || !strings.Contains(text, "unexpected token") {
		t.Fatalf("evaluate error should keep the validation message: %s", text)
	}
	decision = "decline"
	if result, _ := call("update_automation", map[string]any{"id": "a1", "enabled": true}); !result.IsError || putBody != nil {
		t.Fatal("declined update reached the backend")
	}
	decision = "approve"
	result, text := call("update_automation", map[string]any{"id": "a1", "enabled": true})
	if result.IsError || putBody["enabled"] != true || putBody["cooldown_seconds"] != 5.0 || putBody["condition"] != "x > 1" || putBody["timeout_seconds"] != 60.0 {
		t.Fatalf("update merge: %s %v", text, putBody)
	}
	var out struct {
		Backup string `json:"backup"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	saved, err := os.ReadFile(out.Backup)
	if err != nil || !strings.Contains(string(saved), `"cooldown_seconds": 5`) {
		t.Fatalf("backup %q: %s %v", out.Backup, saved, err)
	}
	if result, text := call("write_automation_file", map[string]any{"id": "a1", "path": "../x.ts", "content": "x"}); !result.IsError {
		t.Fatalf("escaping path accepted: %s", text)
	}
	if result, text := call("write_automation_file", map[string]any{"id": "a1", "path": "lib/x.ts", "content": "1;\n"}); result.IsError || !strings.Contains(text, `"backup"`) {
		t.Fatalf("write: %s", text)
	}
	if prompts != 3 {
		t.Fatalf("approval prompts = %d, want 3", prompts)
	}
	mu.Lock()
	defer mu.Unlock()
	want := "GET /api/automations/|POST /api/automations/|POST /api/automations/evaluate|GET /api/automations/a1|PUT /api/automations/a1|GET /api/automations/a1/files/content?path=lib%2Fx.ts|PUT /api/automations/a1/files/content?path=lib%2Fx.ts"
	if strings.Join(calls, "|") != want {
		t.Fatalf("calls:\n%s\nwant\n%s", strings.Join(calls, "|"), want)
	}
}
