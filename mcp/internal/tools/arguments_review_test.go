package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExactToolPreservesSDKDiscoverySchema(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "schema-fixture"}, nil)
	handler := func(context.Context, *mcp.CallToolRequest, client.VMIDInput) (*mcp.CallToolResult, struct {
		Value int64 `json:"value"`
	}, error) {
		return nil, struct {
			Value int64 `json:"value"`
		}{}, nil
	}
	mcp.AddTool(server, &mcp.Tool{Name: "sdk"}, handler)
	addExactTool(server, &mcp.Tool{Name: "exact"}, handler)
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "schema-client"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 2 {
		t.Fatal("missing tools")
	}
	for _, pair := range [][2]any{{result.Tools[0].InputSchema, result.Tools[1].InputSchema}, {result.Tools[0].OutputSchema, result.Tools[1].OutputSchema}} {
		var schemas [2]any
		for i, value := range pair {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &schemas[i]); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(schemas[0], schemas[1]) {
			t.Fatalf("schema changed: %#v", schemas)
		}
	}
}
