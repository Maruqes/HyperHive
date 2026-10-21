package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"512SvMan/internal/testnpm"
	"512SvMan/services"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Real authenticated HyperHive handlers and durable fleet state, without starting
// the reconciler, contacting agents, provisioning buckets or touching production.
func TestCelldMCPActualHandlers(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	statePath := filepath.Join(t.TempDir(), "fleets.json")
	initial := `{"schema":2,"root_user":"fixture-root","root_password":"fixture-private","machines":[{"name":"fixture-node"}],"fleets":{}}`
	if err := os.WriteFile(statePath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	old := services.CelldFleet
	t.Cleanup(func() { services.CelldFleet = old })
	services.CelldFleet = services.NewCelldService(statePath)
	router := chi.NewRouter()
	server := httptest.NewUnstartedServer(router)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	setupMCPAPI(router, port)
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupCelldAPI(r) })
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "celld-handler-contract"}, &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}, nil
	}}).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpBearerTransport{}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any, wantError bool) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError != wantError {
			t.Fatalf("%s error=%v result=%#v", name, err, result)
		}
		if wantError {
			return nil
		}
		data, _ := json.Marshal(result.StructuredContent)
		var envelope struct {
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Result == nil {
			t.Fatalf("%s result %s", name, data)
		}
		return envelope.Result
	}
	list := call("list_celld_fleets", map[string]any{}, false)
	if len(list["fleets"].([]any)) != 0 || list["reset_pending"] != false {
		t.Fatal("initial list")
	}
	health := call("get_celld_health", map[string]any{}, false)
	if health["healthy"] != false || health["storage"].(map[string]any)["error"] != "storage has not been checked" {
		t.Fatal("503 diagnostics lost")
	}
	fleet := call("create_celld_fleet", map[string]any{"name": "fixture", "servers": []string{"fixture-node"}}, false)
	id, ok := fleet["id"].(string)
	if !ok || id == "" || fleet["status"] != "provisioning" || fleet["desired_state"] != "running" {
		t.Fatalf("create: %#v", fleet)
	}
	ports := fleet["ports"].(map[string]any)
	if ports["http"] != float64(18080) || ports["internal"] != float64(18081) {
		t.Fatalf("automatic ports: %#v", ports)
	}
	args := map[string]any{"id": id}
	keys := call("get_celld_credentials", args, false)
	if keys["aws_access_key_id"] == "" || keys["aws_secret_access_key"] == "" {
		t.Fatal("missing scoped credentials")
	}
	call("create_celld_fleet", map[string]any{"name": "fixture", "servers": []string{"fixture-node"}}, true)
	call("create_celld_fleet", map[string]any{"name": "unknown", "servers": []string{"not-in-inventory"}}, true)
	call("start_celld_fleet", args, true) // A newly created fleet is busy.
	for _, kind := range []string{"logs", "events"} {
		page := call("get_celld_"+kind, args, false)
		if page["incomplete"] != true || page["error"] != "observability storage unavailable" || len(page["records"].([]any)) != 0 {
			t.Fatalf("partial %s: %#v", kind, page)
		}
		exported := call("get_celld_"+kind, map[string]any{"id": id, "download": true, "limit": 2000}, false)
		if len(exported["records"].([]any)) != 0 || exported["truncated"] != false {
			t.Fatalf("empty NDJSON: %#v", exported)
		}
	}
	// Simulate completed previous operations by changing only disposable persisted
	// fixture state. No reconciler or actual process operation runs in this test.
	for _, action := range []struct{ name, state, status string }{{"start_celld_fleet", "running", "provisioning"}, {"stop_celld_fleet", "stopped", "provisioning"}} {
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		if json.Unmarshal(data, &saved) != nil {
			t.Fatal("state decode")
		}
		record := saved["fleets"].(map[string]any)[id].(map[string]any)
		record["operation"] = ""
		encoded, _ := json.Marshal(saved)
		if err = os.WriteFile(statePath, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		services.CelldFleet = services.NewCelldService(statePath)
		result := call(action.name, args, false)
		if result["desired_state"] != action.state || result["status"] != action.status || result["id"] != id {
			t.Fatalf("%s: %#v", action.name, result)
		}
	}
	call("get_celld_credentials", args, false) // Stopped fleets retain their scoped credentials.
	call("stop_celld_fleet", map[string]any{"id": "missing"}, true)
	final := call("list_celld_fleets", map[string]any{}, false)
	if len(final["fleets"].([]any)) != 1 {
		t.Fatal("start/stop must retain the fleet")
	}
	for _, secret := range []string{keys["aws_access_key_id"].(string), keys["aws_secret_access_key"].(string), "fixture-private"} {
		data, _ := json.Marshal(final)
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("list leaked credentials")
		}
	}
}
