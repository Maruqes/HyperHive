package hyperhivemcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPApprovalProtocolCompatibility(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		for _, decision := range []string{"approve", "cancel", "decline", "unsupported"} {
			t.Run(version+"/"+decision, func(t *testing.T) {
				server := mcp.NewServer(&mcp.Implementation{Name: "transport-policy"}, nil)
				gate := &vmsafety.Gate{}
				var writes, prompts atomic.Int32
				mcp.AddTool(server, &mcp.Tool{Name: "protected_change"}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
					result, err := gate.Require(ctx, req, vmsafety.High, "Change disposable fixture setting")
					if result != nil || err != nil {
						return result, nil, err
					}
					writes.Add(1)
					return nil, map[string]any{"success": true}, nil
				})
				endpoint := httptest.NewServer(requireMCPToken(newMCPTransport(server), "compatibility-fixture"))
				defer endpoint.Close()
				opts := &mcp.ClientOptions{}
				if decision != "unsupported" {
					opts.ElicitationHandler = func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
						prompts.Add(1)
						if decision == "decline" {
							return &mcp.ElicitResult{Action: "decline"}, nil
						}
						return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": decision}}, nil
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				client := mcp.NewClient(&mcp.Implementation{Name: "transport-policy-client"}, opts)
				session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"compatibility-fixture"}}}, &mcp.ClientSessionOptions{ProtocolVersion: version})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "protected_change", Arguments: map[string]any{}})
				if decision == "approve" {
					if err != nil || result.IsError || writes.Load() != 1 || prompts.Load() != 1 {
						t.Fatalf("approval failed: %v %#v writes=%d prompts=%d", err, result, writes.Load(), prompts.Load())
					}
				} else if writes.Load() != 0 {
					t.Fatal("unapproved operation executed")
				}
			})
		}
	}
}
