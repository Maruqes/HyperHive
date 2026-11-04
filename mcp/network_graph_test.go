package hyperhivemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func networkGraphSession(t *testing.T, ctx context.Context, body string, status int, calls *[]string) *mcp.ClientSession {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer graph-fixture-key" {
			t.Error("missing API auth")
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(backend.Close)
	reviewDir := t.TempDir()
	if err := os.Chmod(reviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/api", MCPToken: "graph-fixture-key", SafetyReviewDir: reviewDir}))
	t.Cleanup(endpoint.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "graph-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"graph-fixture-key"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestNetworkGraphReadOnlyContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var calls []string
	session := networkGraphSession(t, ctx, `{"generatedAt":"2026-11-04T12:00:00Z","nodes":[{"id":"vm:a:web","type":"vm","label":"web","host":"a","status":"up","details":{"addresses":["192.0.2.5"]}},{"id":"proxy:1","type":"proxy","label":"a.example","host":"a","status":"up","details":{}}],"edges":[{"source":"proxy:1","target":"vm:a:web","type":"forwards","match":"ip"}],"warnings":["proxies timed out"]}`, http.StatusOK, &calls)

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, tool := range listed.Tools {
		if strings.Contains(tool.Name, "network_graph") {
			found = append(found, tool.Name)
			if tool.Meta["hyperhive/risk"] != "low" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s is not low-risk read-only: %v %#v", tool.Name, tool.Meta["hyperhive/risk"], tool.Annotations)
			}
		}
	}
	if strings.Join(found, ",") != "network_graph_get" {
		t.Fatalf("network tools = %v", found)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "network_graph_get", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("get: %v %#v", err, result)
	}
	if strings.Join(calls, "|") != "GET /api/network/graph" {
		t.Fatalf("calls = %v", calls)
	}
	var out struct {
		Nodes []struct {
			ID      string         `json:"id"`
			Details map[string]any `json:"details"`
		} `json:"nodes"`
		Edges []struct {
			Match string `json:"match"`
		} `json:"edges"`
		Warnings []string `json:"warnings"`
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Nodes) != 2 || out.Nodes[0].Details["addresses"] == nil || len(out.Edges) != 1 || out.Edges[0].Match != "ip" || len(out.Warnings) != 1 {
		t.Fatalf("output = %s", data)
	}
}

func TestNetworkGraphRejectsInvalidResponsesAndArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for name, fixture := range map[string]struct {
		body   string
		status int
	}{
		"missing arrays": {`{"generatedAt":"2026-11-04T12:00:00Z"}`, http.StatusOK},
		"server error":   {`secret upstream body`, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			var calls []string
			session := networkGraphSession(t, ctx, fixture.body, fixture.status, &calls)
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "network_graph_get", Arguments: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("expected a tool error: %#v", result)
			}
			if data, _ := json.Marshal(result.Content); strings.Contains(string(data), "secret upstream body") {
				t.Fatalf("backend body leaked: %s", data)
			}
		})
	}

	var calls []string
	session := networkGraphSession(t, ctx, `{}`, http.StatusOK, &calls)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "network_graph_get", Arguments: map[string]any{"unexpected": true}})
	if err == nil && !result.IsError {
		t.Fatalf("unknown argument accepted: %#v", result)
	}
	if len(calls) != 0 {
		t.Fatalf("invalid arguments reached the API: %v", calls)
	}
}
