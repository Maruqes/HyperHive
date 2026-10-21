package tools

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixtureTool(name string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: "Change a disposable note.", Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": "POST", "path": "/fixture"}}}
}

func registerFixtureTool[In, Out any](r *Registry, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out], level safety.Level) error {
	return RegisterTool(r, tool.Name, defineTool(tool, handler), level)
}

func TestRegistryRejectsInvalidAndDuplicateDefinitions(t *testing.T) {
	handler := func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	}
	for _, level := range []safety.Level{"", "unknown"} {
		r := NewRegistry(mcp.NewServer(&mcp.Implementation{Name: "fixture"}, nil), "").Area("notes")
		if registerFixtureTool(r, fixtureTool("fixture"), handler, level) == nil || r.Err() == nil {
			t.Fatal("invalid classification accepted")
		}
	}
	r := NewRegistry(mcp.NewServer(&mcp.Implementation{Name: "fixture"}, nil), "").Area("notes")
	if err := registerFixtureTool(r, fixtureTool("fixture"), handler, safety.Low); err != nil {
		t.Fatal(err)
	}
	if registerFixtureTool(r, fixtureTool("fixture"), handler, safety.High) == nil || r.Err() == nil {
		t.Fatal("duplicate tool accepted")
	}
	r = NewRegistry(mcp.NewServer(&mcp.Implementation{Name: "fixture"}, nil), "").Area("notes")
	if RegisterTool(r, "different_name", defineTool(fixtureTool("fixture"), handler), safety.Low) == nil || r.Err() == nil {
		t.Fatal("catalog name differed from the registered definition")
	}
}

func TestSharedPolicyLevelsAndPreflightPersistence(t *testing.T) {
	for _, level := range []safety.Level{safety.Low, safety.Medium, safety.High, safety.VeryHigh} {
		for _, decision := range []string{"approve", "cancel", "unsupported"} {
			t.Run(string(level)+"/"+decision, func(t *testing.T) {
				server := mcp.NewServer(&mcp.Implementation{Name: "policy-fixture"}, nil)
				dir := filepath.Join(t.TempDir(), "reviews")
				r := NewRegistry(server, dir).Area("notes")
				var calls, prompts atomic.Int32
				err := registerFixtureTool(r, fixtureTool("fixture"), func(_ context.Context, _ *mcp.CallToolRequest, input client.EditNoteInput) (*mcp.CallToolResult, any, error) {
					calls.Add(1)
					if level == safety.Medium {
						entries, err := os.ReadDir(dir)
						if err != nil || len(entries) != 1 {
							t.Error("preflight was not durable before dispatch")
						}
						data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
						var review safety.Review
						if json.Unmarshal(data, &review) != nil || review.ArgumentsSHA256 == "" || review.Target != "id=9007199254740993" || review.Outcome != "preflight_passed" {
							t.Errorf("incomplete preflight: %s", data)
						}
						if strings.Contains(string(data), "secret-note-body") {
							t.Error("private note content leaked into review")
						}
					}
					return nil, map[string]any{"id": input.ID}, nil
				}, level)
				if err != nil {
					t.Fatal(err)
				}
				endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
				defer endpoint.Close()
				opts := &mcp.ClientOptions{}
				if decision != "unsupported" {
					opts.ElicitationHandler = func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
						prompts.Add(1)
						if strings.Contains(req.Params.Message, "secret-note-body") {
							t.Error("approval leaked note body")
						}
						return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": decision}}, nil
					}
				}
				session, err := mcp.NewClient(&mcp.Implementation{Name: "policy-client"}, opts).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "fixture", Arguments: json.RawMessage(`{"id":9007199254740993,"titulo":"private title","nota":"secret-note-body"}`)})
				shouldRun := level == safety.Low || level == safety.Medium || decision == "approve"
				if shouldRun {
					if err != nil || out.IsError || calls.Load() != 1 {
						t.Fatalf("valid call failed: %v %#v", err, out)
					}
				} else if calls.Load() != 0 {
					t.Fatal("unapproved execution")
				}
				if (level == safety.Low || level == safety.Medium) && prompts.Load() != 0 {
					t.Fatal("Low/Medium prompted")
				}
			})
		}
	}
}

func TestInvalidMediumInputAndReviewFailureCannotDispatch(t *testing.T) {
	for _, failure := range []string{"input", "storage"} {
		t.Run(failure, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "failure-fixture"}, nil)
			dir := filepath.Join(t.TempDir(), "reviews")
			if failure == "storage" {
				if err := os.WriteFile(dir, []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := NewRegistry(server, dir).Area("notes")
			var calls atomic.Int32
			registerFixtureTool(r, fixtureTool("fixture"), func(context.Context, *mcp.CallToolRequest, client.NoteContent) (*mcp.CallToolResult, any, error) {
				calls.Add(1)
				return nil, nil, nil
			}, safety.Medium)
			endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
			defer endpoint.Close()
			session, err := mcp.NewClient(&mcp.Implementation{Name: "failure-client"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			title := "valid"
			if failure == "input" {
				title = " "
			}
			out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "fixture", Arguments: map[string]any{"titulo": title, "nota": "private"}})
			if calls.Load() != 0 || err == nil && !out.IsError {
				t.Fatal("invalid/unrecorded Medium operation dispatched")
			}
		})
	}
}

func TestNoProductionRegistrationBypasses(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "AddTool" || sel.Sel.Name == "RemoveTools") && name != "arguments.go" {
				t.Errorf("%s bypasses shared registration", name)
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "addExactTool" && name != "registry.go" {
				t.Errorf("%s bypasses safety policy", name)
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "registerTypedTool" && name != "registry.go" {
				t.Errorf("%s bypasses the area catalog", name)
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "RegisterTool" {
				inCatalog := false
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if ok && strings.HasPrefix(fn.Name.Name, "Register") && fn.Body != nil && call.Pos() > fn.Body.Pos() && call.End() < fn.Body.End() {
						inCatalog = true
					}
				}
				if !inCatalog {
					t.Errorf("%s declares a tool outside its area Register function", name)
				}
			}
			return true
		})
	}
}
