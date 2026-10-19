package hyperhivemcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Discovery is part of the safety boundary: callers cannot supply a lower risk,
// self-approve in arguments or reach an excluded deletion through an alias.
func TestVMPolicyDiscovery(t *testing.T) {
	endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: "http://127.0.0.1:1", MCPToken: "policy-fixture"}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "policy-review"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"policy-fixture"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tool := range listed.Tools {
		raw, _ := json.Marshal(tool.Meta["hyperhive/endpoint"])
		var mapping struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		}
		if json.Unmarshal(raw, &mapping) != nil {
			t.Fatalf("missing endpoint mapping for %s", tool.Name)
		}
		if !strings.HasPrefix(mapping.Path, "/virsh/") && !strings.HasPrefix(mapping.Path, "/vm-disk/") {
			continue
		}
		count++
		risk, _ := tool.Meta["hyperhive/risk"].(string)
		if risk != "low" && risk != "medium" && risk != "high" && risk != "very high" {
			t.Errorf("%s missing risk", tool.Name)
		}
		if !strings.Contains(tool.Description, "Risk: "+risk) {
			t.Errorf("%s does not describe risk", tool.Name)
		}
		if mapping.Method != "GET" && risk == "low" {
			t.Errorf("VM mutation %s is rated low", tool.Name)
		}
		if mapping.Method == "DELETE" || strings.Contains(strings.ToLower(tool.Name), "delete") || strings.Contains(strings.ToLower(tool.Name), "remove") {
			t.Errorf("new deletion capability exposed: %s", tool.Name)
		}
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || mapping.Method != "GET" && (!*tool.Annotations.DestructiveHint || tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s unsafe annotations", tool.Name)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		schemaRaw, _ := json.Marshal(tool.InputSchema)
		json.Unmarshal(schemaRaw, &schema)
		for _, forbidden := range []string{"approved", "confirmed", "confirm", "risk", "method", "endpoint", "action"} {
			if _, ok := schema.Properties[forbidden]; ok {
				t.Errorf("%s exposes authority/dispatcher argument %s", tool.Name, forbidden)
			}
		}
	}
	if count == 0 {
		t.Fatal("no VM tools discovered")
	}
}
