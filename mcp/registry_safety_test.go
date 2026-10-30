package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMediumReviewAndProtectedDNSAcrossTransports(t *testing.T) {
	for _, transport := range []string{"http", "stdio", "legacy-http", "legacy-stdio"} {
		t.Run(transport, func(t *testing.T) {
			backup := filepath.Join(t.TempDir(), "recovery")
			reviews := backup + "-reviews"
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/prefix/api/notes/" {
					t.Errorf("unexpected backend operation: %s %s", r.Method, r.URL)
					return
				}
				entries, err := os.ReadDir(reviews)
				if err != nil || len(entries) != 1 {
					t.Error("Medium dispatched without persisted review")
					return
				}
				data, _ := os.ReadFile(filepath.Join(reviews, entries[0].Name()))
				var review safety.Review
				if json.Unmarshal(data, &review) != nil || review.Tool != "create_note" || review.ExpectedImpact == "" || len(review.Checks) == 0 {
					t.Error("incomplete review")
				}
				if strings.Contains(string(data), "private-note") {
					t.Error("review leaked note content")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"id":9007199254740993,"titulo":"title","nota":"private-note"}`)
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, backup, nil)
			out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_note", Arguments: map[string]any{"titulo": "title", "nota": "private-note"}})
			if err != nil || out.IsError || calls.Load() != 1 {
				t.Fatalf("Medium required approval or failed: %v %#v calls=%d", err, out, calls.Load())
			}
			_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_alias_add", Arguments: map[string]any{"alias": "fixture.test", "ip": "192.0.2.1"}})
			if calls.Load() != 1 {
				t.Fatal("protected DNS call reached backend without approval")
			}
			_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_celld_fleet", Arguments: map[string]any{"id": "fixture"}})
			if calls.Load() != 1 {
				t.Fatal("removed deletion call reached backend")
			}
		})
	}
}

// The optional export is used only by an explicitly requested local inventory
// refresh. Ordinary tests verify discovery without modifying documentation.
func TestUnifiedSafetyDiscoveryInventory(t *testing.T) {
	server, err := NewServer(Config{BaseURL: "http://127.0.0.1:1", MCPToken: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "inventory-fixture"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 119 {
		t.Fatalf("unexpected tool inventory: %d", len(listed.Tools))
	}
	// Every live tool must have exactly one literal name and level in a compact
	// area Register function; helper factories cannot hide registrations.
	catalogLevels := map[string]string{}
	catalogAreas := map[string]string{}
	files := []string{"vms.go", "celld.go", "dns.go", "isos.go", "notes.go", "notifications.go", "spa.go", "proxies.go", "nginx.go", "upstreams.go"}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("internal/tools", path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		area := strings.TrimSuffix(filepath.Base(path), ".go")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Register") {
				continue
			}
			if decl != file.Decls[len(file.Decls)-1] {
				t.Errorf("%s registration must be at the end of the file", path)
			}
			for _, statement := range fn.Body.List {
				expression, ok := statement.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := expression.X.(*ast.CallExpr)
				if !ok {
					continue
				}
				callee, ok := call.Fun.(*ast.Ident)
				if !ok || callee.Name != "RegisterTool" || len(call.Args) != 4 {
					t.Errorf("%s hides a registration", fn.Name.Name)
					continue
				}
				name, nameOK := call.Args[1].(*ast.BasicLit)
				level, levelOK := call.Args[3].(*ast.SelectorExpr)
				if !nameOK || !levelOK {
					t.Errorf("%s must show literal tool name and level", path)
					continue
				}
				toolName, err := strconv.Unquote(name.Value)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := catalogLevels[toolName]; exists {
					t.Errorf("duplicate catalog entry %s", toolName)
				}
				catalogLevels[toolName] = map[string]string{"Low": "low", "Medium": "medium", "High": "high", "VeryHigh": "very high"}[level.Sel.Name]
				catalogAreas[toolName] = area
			}
		}
	}
	if len(catalogLevels) != len(listed.Tools) {
		t.Fatalf("catalog declares %d tools; discovery has %d", len(catalogLevels), len(listed.Tools))
	}
	sort.Slice(listed.Tools, func(i, j int) bool {
		a, b := listed.Tools[i], listed.Tools[j]
		return fmt.Sprint(a.Meta["hyperhive/area"], a.Name) < fmt.Sprint(b.Meta["hyperhive/area"], b.Name)
	})
	var rows strings.Builder
	rows.WriteString("| Area | Tool | Level | API endpoint |\n| --- | --- | --- | --- |\n")
	for _, tool := range listed.Tools {
		level := safety.Level(fmt.Sprint(tool.Meta["hyperhive/risk"]))
		area, _ := tool.Meta["hyperhive/area"].(string)
		if catalogLevels[tool.Name] != string(level) || catalogAreas[tool.Name] != area {
			t.Errorf("%s differs from its visible catalog policy", tool.Name)
		}
		if !level.Valid() || area == "" || tool.Meta["hyperhive/required_action"] != level.RequiredAction() || !strings.Contains(tool.Description, "Risk: "+string(level)) {
			t.Errorf("invalid policy metadata: %s", tool.Name)
		}
		endpoint, ok := tool.Meta["hyperhive/endpoint"].(map[string]any)
		if !ok {
			t.Fatalf("invalid endpoint for %s", tool.Name)
		}
		if endpoint["method"] == "DELETE" || tool.Name == "delete_celld_fleet" {
			t.Error("deletion registration remains")
		}
		fmt.Fprintf(&rows, "| %s | `%s` | %s | `%v %v` |\n", area, tool.Name, level, endpoint["method"], endpoint["path"])
	}
	if path := os.Getenv("HH_WRITE_SAFETY_INVENTORY"); path != "" {
		if err := os.WriteFile(path, []byte(rows.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
