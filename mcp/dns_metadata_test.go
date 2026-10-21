package hyperhivemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDNSOptionalMetadataContracts(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var mu sync.Mutex
			var writes []map[string]any
			response := `{"aliases":[{"alias":"legacy.test","ip":"192.0.2.1","ssh_username":"","related_vm":""},{"alias":"app.test","ip":"192.0.2.2","ssh_username":"ubuntu","related_vm":"guest-one","extension":"preserved"}]}`
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer vm-fixture" {
					t.Error("missing backend authentication")
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /prefix/api/dnsmasq/alias/all":
					mu.Lock()
					defer mu.Unlock()
					io.WriteString(w, response)
				case "POST /prefix/api/dnsmasq/alias/add":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					mu.Lock()
					writes = append(writes, body)
					mu.Unlock()
				default:
					t.Errorf("unexpected backend call: %s %s", r.Method, r.URL)
				}
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, t.TempDir(), &mcp.ClientOptions{ElicitationHandler: vmApprove})
			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, tool := range listed.Tools {
				if tool.Name != "dns_alias_add" && tool.Name != "dns_alias_get" && tool.Name != "dns_aliases_list" {
					continue
				}
				data, _ := json.Marshal(tool.InputSchema)
				var schema struct {
					Properties map[string]any
					Required   []string
				}
				if err := json.Unmarshal(data, &schema); err != nil {
					t.Fatal(err)
				}
				if tool.Name == "dns_alias_add" {
					for _, key := range []string{"ssh_username", "related_vm"} {
						if schema.Properties[key] == nil {
							t.Fatalf("add discovery missing optional %s", key)
						}
						for _, required := range schema.Required {
							if required == key {
								t.Fatalf("%s must remain optional", key)
							}
						}
					}
				} else if tool.Name == "dns_alias_get" {
					if len(schema.Properties) != 2 {
						t.Fatal("existence check gained metadata inputs")
					}
				} else {
					data, _ := json.Marshal(tool.OutputSchema)
					var output struct {
						Properties map[string]struct {
							Items struct{ Properties map[string]any }
						}
					}
					if err := json.Unmarshal(data, &output); err != nil {
						t.Fatal(err)
					}
					for _, key := range []string{"ssh_username", "related_vm"} {
						if output.Properties["aliases"].Items.Properties[key] == nil {
							t.Fatalf("list discovery missing %s", key)
						}
					}
				}
			}
			for _, extra := range []map[string]any{
				{}, {"ssh_username": "ubuntu"}, {"related_vm": "guest-one"},
				{"ssh_username": "ubuntu", "related_vm": "guest-one"},
				{"ssh_username": "", "related_vm": ""},
				{"ssh_username": "manual \"value\"\nwith spaces"},
			} {
				args := map[string]any{"alias": "app.test", "ip": "192.0.2.2"}
				for key, value := range extra {
					args[key] = value
				}
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_alias_add", Arguments: args})
				if err != nil || out.IsError {
					t.Fatalf("optional metadata rejected: %+v, %v", out, err)
				}
				mu.Lock()
				got := writes[len(writes)-1]
				mu.Unlock()
				if !reflect.DeepEqual(got, args) {
					t.Fatalf("POST metadata changed: got %+v, want %+v", got, args)
				}
				var result map[string]bool
				if err := json.Unmarshal([]byte(out.Content[0].(*mcp.TextContent).Text), &result); err != nil || !reflect.DeepEqual(result, map[string]bool{"success": true}) {
					t.Fatalf("add must keep acknowledgement-only output: %+v, %v", result, err)
				}
			}
			for _, key := range []string{"ssh_username", "related_vm"} {
				for _, value := range []any{nil, false, 123, []any{}, map[string]any{}} {
					mu.Lock()
					before := len(writes)
					mu.Unlock()
					out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_alias_add", Arguments: map[string]any{"alias": "app.test", "ip": "192.0.2.2", key: value}})
					if err == nil && !out.IsError {
						t.Fatalf("invalid metadata accepted: %s=%v", key, value)
					}
					mu.Lock()
					after := len(writes)
					mu.Unlock()
					if after != before {
						t.Fatal("invalid metadata reached backend")
					}
				}
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_alias_get", Arguments: map[string]any{"alias": "app.test", "ip": "192.0.2.2", key: "not-a-get-input"}})
				if err == nil && !out.IsError {
					t.Fatal("get accepted write-only metadata")
				}
			}
			out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_aliases_list", Arguments: map[string]any{}})
			if err != nil || out.IsError {
				t.Fatalf("list failed: %+v, %v", out, err)
			}
			var got, want any
			json.Unmarshal([]byte(out.Content[0].(*mcp.TextContent).Text), &got)
			json.Unmarshal([]byte(response), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("list lost metadata or extension fields: %+v", got)
			}
			data, err := json.Marshal(out.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("structured list differs from text response: %s, %v", data, err)
			}
			for _, bad := range []string{
				`{"aliases":[{"alias":"app.test","ip":"192.0.2.2","ssh_username":123,"related_vm":"guest-one"}]}`,
				`{"aliases":[{"alias":"app.test","ip":"192.0.2.2","ssh_username":"user","related_vm":null}]}`,
			} {
				mu.Lock()
				response = bad
				mu.Unlock()
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "dns_aliases_list", Arguments: map[string]any{}})
				if err == nil && !out.IsError {
					t.Fatalf("invalid response metadata accepted: %s", bad)
				}
			}
		})
	}
}
