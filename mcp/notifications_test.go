package hyperhivemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNotificationRulesContracts(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Header.Get("Authorization") != "Bearer rules-fixture-key" {
			t.Error("missing API auth")
		}
		data, _ := io.ReadAll(r.Body)
		gotBody = nil
		_ = json.Unmarshal(data, &gotBody)
		if r.Method == "GET" {
			io.WriteString(w, `[{"category":"metrics","description":"d","enabled":true,"minPushSeverity":"error","quietStart":"","quietEnd":""}]`)
			return
		}
		io.WriteString(w, `{"category":"metrics","description":"d","enabled":false,"minPushSeverity":"warning","quietStart":"22:00","quietEnd":"07:00"}`)
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reviewDir := t.TempDir()
	if err := os.Chmod(reviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/api", MCPToken: "rules-fixture-key", SafetyReviewDir: reviewDir}))
	defer endpoint.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "rules-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"rules-fixture-key"}}}, nil)
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
		if strings.Contains(tool.Name, "notification") {
			risks[tool.Name] = tool.Meta["hyperhive/risk"]
		}
	}
	if !reflect.DeepEqual(risks, map[string]any{"list_notification_rules": "low", "update_notification_rule": "medium"}) {
		t.Fatalf("notification tools: %v", risks)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_notification_rules", Arguments: map[string]any{}})
	if err != nil || result.IsError || gotMethod != "GET" || gotPath != "/api/notification/rules" {
		t.Fatalf("list: %v %#v %s %s", err, result, gotMethod, gotPath)
	}

	args := map[string]any{"category": "metrics", "enabled": false, "minPushSeverity": "warning", "quietStart": "22:00", "quietEnd": "07:00"}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "update_notification_rule", Arguments: args})
	if err != nil || result.IsError || gotMethod != "PUT" || gotPath != "/api/notification/rules/metrics" {
		t.Fatalf("update: %v %s %s %s", err, result.Content[0].(*mcp.TextContent).Text, gotMethod, gotPath)
	}
	if !reflect.DeepEqual(gotBody, map[string]any{"enabled": false, "minPushSeverity": "warning", "quietStart": "22:00", "quietEnd": "07:00"}) {
		t.Fatalf("body: %v", gotBody)
	}

	gotMethod = ""
	for _, bad := range []map[string]any{
		{"category": "nope", "enabled": true, "minPushSeverity": "error"},
		{"category": "metrics", "enabled": true, "minPushSeverity": "urgent"},
		{"category": "metrics", "enabled": true, "minPushSeverity": "error", "quietStart": "22:00"},
	} {
		result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "update_notification_rule", Arguments: bad})
		if err == nil && !result.IsError {
			t.Fatalf("invalid input accepted: %v", bad)
		}
	}
	if gotMethod != "" {
		t.Fatal("invalid input reached the API")
	}
}
