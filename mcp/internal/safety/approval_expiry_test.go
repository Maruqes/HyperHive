package safety

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApprovalExpiryAndDifferentClientBinding(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "binding-fixture"}, nil)
	gate := &Gate{}
	var calls atomic.Int32
	mcp.AddTool(server, &mcp.Tool{Name: "protected"}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		pending, err := gate.Require(ctx, req, High, "Change disposable fixture")
		if pending != nil || err != nil {
			return pending, nil, err
		}
		calls.Add(1)
		return nil, map[string]any{"success": true}, nil
	})
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer endpoint.Close()
	connect := func(name string) *mcp.ClientSession {
		t.Helper()
		session, err := mcp.NewClient(&mcp.Implementation{Name: name}, &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}}).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	first, other := connect("first-client"), connect("other-client")
	start := func() string {
		t.Helper()
		out, err := first.CallTool(context.Background(), &mcp.CallToolParams{Name: "protected", Arguments: map[string]any{}})
		if err != nil || !out.NeedsInput() {
			t.Fatalf("missing approval: %v %#v", err, out)
		}
		return out.RequestState
	}
	approve := func(session *mcp.ClientSession, token string) {
		t.Helper()
		out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "protected", Arguments: map[string]any{}, RequestState: token, InputResponses: mcp.InputResponseMap{"tool_approval": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}}})
		if err == nil && !out.IsError || calls.Load() != 0 {
			t.Fatalf("invalid continuation executed: %v %#v", err, out)
		}
	}
	token := start()
	gate.mu.Lock()
	pending := gate.pending[token]
	pending.expires = time.Now().Add(-time.Second)
	gate.pending[token] = pending
	gate.mu.Unlock()
	approve(first, token)
	approve(other, start())
}

func TestApprovalRejectsUnexpectedResponseFields(t *testing.T) {
	if approvalResponseError(&mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve", "human": true}}) == nil {
		t.Fatal("extra approval fields accepted")
	}
}
