package hyperhivemcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewProxyListRetainsExactIntegerIDs(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/prefix/api/proxy/list" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, `[{"id":9007199254740993,"meta":{"counter":9223372036854775807},"enabled":false}]`)
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), nil)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_proxies", Arguments: map[string]any{}})
			if err != nil || result.IsError {
				t.Fatalf("list failed: %v %#v", err, result)
			}
			if len(result.Content) != 1 {
				t.Fatalf("missing text fallback: %#v", result.Content)
			}
			text, ok := result.Content[0].(*mcp.TextContent)
			if !ok || !strings.Contains(text.Text, `"id":9007199254740993`) || !strings.Contains(text.Text, `"counter":9223372036854775807`) {
				t.Fatalf("proxy identifiers rounded: %#v", result.Content)
			}
		})
	}
}
