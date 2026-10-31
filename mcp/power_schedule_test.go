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

func TestPowerScheduleReadOnlyContract(t *testing.T) {
	var calls []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer power-fixture-key" {
			t.Error("missing API auth")
		}
		io.WriteString(w, `{"schedule":{"enabled":true,"start":"08:00","stop":"01:00","tz":"Europe/Lisbon","updatedAt":"2026-10-31T10:00:00Z"},"masterTz":"Europe/Lisbon","masterTime":"2026-10-31T12:00:00Z","next":{"stopUnix":1793494800,"masterStopUnix":1793494980,"wakeUnix":1793520000},"hosts":[{"machine":"marques512sv","connected":true,"enabled":true,"stopUnix":1793494980,"wakeUnix":1793520000,"jobId":"12","appliedAt":"2026-10-31T10:00:01Z","lastError":"","attemptedAt":"2026-10-31T10:00:01Z"}]}`)
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reviewDir := t.TempDir()
	if err := os.Chmod(reviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/api", MCPToken: "power-fixture-key", SafetyReviewDir: reviewDir}))
	defer endpoint.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "power-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"power-fixture-key"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, tool := range listed.Tools {
		if strings.Contains(tool.Name, "power") {
			found = append(found, tool.Name)
			if tool.Meta["hyperhive/risk"] != "low" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s is not low-risk read-only: %v %#v", tool.Name, tool.Meta["hyperhive/risk"], tool.Annotations)
			}
		}
	}
	if strings.Join(found, ",") != "get_power_schedule" {
		t.Fatalf("power tools = %v; only the read-only getter may exist", found)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_power_schedule", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("get: %v %#v", err, result)
	}
	if strings.Join(calls, "|") != "GET /api/power-schedule" {
		t.Fatalf("calls = %v", calls)
	}
	var out struct {
		Result struct {
			Schedule struct{ Stop string } `json:"schedule"`
			Hosts    []struct {
				JobID string `json:"jobId"`
			} `json:"hosts"`
		} `json:"result"`
	}
	data, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &out); err != nil || out.Result.Schedule.Stop != "01:00" || len(out.Result.Hosts) != 1 || out.Result.Hosts[0].JobID != "12" {
		t.Fatalf("output %s: %v", data, err)
	}

	for _, name := range []string{"set_power_schedule", "update_power_schedule", "disable_power_schedule"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !result.IsError {
			t.Fatalf("%s must not exist", name)
		}
	}
}
