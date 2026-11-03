package hyperhivemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiskHealthReadOnlyContract(t *testing.T) {
	var calls []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer disk-fixture-key" {
			t.Error("missing API auth")
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/api/diskhealth/":
			io.WriteString(w, `{"lastPollAt":"2026-11-03T12:00:00Z","disks":[{"serial":"WD 1","model":"WD Red","machineName":"marques512sv","device":"/dev/sda","transport":"sata","rotational":true,"capacityBytes":4000000000000,"firstSeen":"2026-11-01T00:00:00Z","lastSeen":"2026-11-03T12:00:00Z","retiredAt":null,"smartAvailable":true,"score":75,"band":"watch","wearOutAt":null,"reasons":["3 pending sectors (+2 in 7 days)"],"pastRatedEndurance":false,"latest":{"capturedAt":"2026-11-03T12:00:00Z","pendingSectors":3,"smartPassed":true}}]}`)
		case "/api/diskhealth/WD 1":
			io.WriteString(w, `{"disk":{"serial":"WD 1","band":"watch","score":75,"reasons":["3 pending sectors"]},"points":[{"at":"2026-08-01T00:00:00Z","resolution":"daily","score":100,"band":"good"},{"at":"2026-11-03T12:00:00Z","resolution":"6h","score":75,"band":"watch","pendingSectors":3}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reviewDir := t.TempDir()
	if err := os.Chmod(reviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend.URL + "/api", MCPToken: "disk-fixture-key", SafetyReviewDir: reviewDir}))
	defer endpoint.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "disk-health-contract"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"disk-fixture-key"}}}, nil)
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
		if strings.Contains(tool.Name, "disk_health") {
			found = append(found, tool.Name)
			if tool.Meta["hyperhive/risk"] != "low" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint {
				t.Fatalf("%s is not low-risk read-only: %v %#v", tool.Name, tool.Meta["hyperhive/risk"], tool.Annotations)
			}
		}
	}
	sort.Strings(found)
	if strings.Join(found, ",") != "get_disk_health_history,list_disk_health" {
		t.Fatalf("disk health tools = %v; only the two read-only tools may exist", found)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_disk_health", Arguments: map[string]any{"state": "active"}})
	if err != nil || result.IsError {
		t.Fatalf("list: %v %#v", err, result)
	}
	var listOut struct {
		Result struct {
			Disks []struct {
				Serial  string   `json:"serial"`
				Score   *int     `json:"score"`
				Reasons []string `json:"reasons"`
			} `json:"disks"`
		} `json:"result"`
	}
	data, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &listOut); err != nil || len(listOut.Result.Disks) != 1 || listOut.Result.Disks[0].Score == nil || *listOut.Result.Disks[0].Score != 75 {
		t.Fatalf("list output %s: %v", data, err)
	}

	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_disk_health_history", Arguments: map[string]any{"serial": "WD 1", "days": 30}})
	if err != nil || result.IsError {
		t.Fatalf("history: %v %#v", err, result)
	}
	var historyOut struct {
		Result struct {
			Points []struct {
				Resolution string `json:"resolution"`
			} `json:"points"`
		} `json:"result"`
	}
	data, _ = json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &historyOut); err != nil || len(historyOut.Result.Points) != 2 || historyOut.Result.Points[0].Resolution != "daily" {
		t.Fatalf("history output %s: %v", data, err)
	}

	// Invalid input never reaches the API.
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_disk_health", Arguments: map[string]any{"state": "bogus"}})
	if err == nil && !result.IsError {
		t.Fatal("invalid state accepted")
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_disk_health_history", Arguments: map[string]any{"serial": " "}})
	if err == nil && !result.IsError {
		t.Fatal("empty serial accepted")
	}

	if want := "GET /api/diskhealth/?state=active|GET /api/diskhealth/WD%201?days=30"; strings.Join(calls, "|") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}

	// The purge endpoint is excluded by the no-deletion policy.
	for _, name := range []string{"purge_disk_health", "delete_disk_health", "remove_disk_health", "purge_retired_disk"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"serial": "WD 1"}})
		if err == nil && !result.IsError {
			t.Fatalf("%s must not exist", name)
		}
	}
}
