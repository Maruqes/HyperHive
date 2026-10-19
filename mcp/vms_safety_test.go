package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func vmApprove(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}, nil
}
func vmPinningCase() vmWriteCase {
	for _, tc := range vmWriteCases {
		if tc.Name == "set_vm_cpu_pinning" {
			return tc
		}
	}
	panic("missing pinning contract")
}
func TestVMSnapshotFailuresCannotWrite(t *testing.T) {
	cases := []string{"pin-fraction", "pin-overflow", "pin-null-entry", "pin-fraction-vcpu", "pin-invalid-bool", "secure-inactive-missing", "secure-current-missing", "secure-identity", "save-fail", "missing-picker", "xml-uuid", "host-mismatch"}
	for _, transport := range []string{"http", "stdio"} {
		for _, failure := range cases {
			t.Run(transport+"/"+failure, func(t *testing.T) {
				tc := vmPinningCase()
				var writes atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						return
					}
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					raw, _ := vmSnapshotFixture(path, tc)
					if path == "/virsh/cpupinning/guest" {
						switch failure {
						case "pin-fraction":
							raw = strings.Replace(raw, `"rangeStart":0`, `"rangeStart":0.5`, 1)
						case "pin-overflow":
							raw = strings.Replace(raw, `"rangeEnd":0`, `"rangeEnd":2147483648`, 1)
						case "pin-null-entry":
							raw = strings.Replace(raw, `"pins":[]`, `"pins":[null]`, 1)
						case "pin-fraction-vcpu":
							raw = strings.Replace(raw, `"pins":[]`, `"pins":[{"vcpu":0.5,"cpuset":"0","isHt":false}]`, 1)
						case "pin-invalid-bool":
							raw = strings.Replace(raw, `"pins":[]`, `"pins":[{"vcpu":0,"cpuset":"0","isHt":1}]`, 1)
						}
					}
					if path == "/virsh/snapshot/guest" {
						switch failure {
						case "secure-inactive-missing":
							raw = strings.Replace(raw, `"inactive_xml":`, `"missing":`, 1)
						case "secure-current-missing":
							raw = strings.Replace(raw, `"active":false`, `"active":true`, 1)
						case "secure-identity":
							raw = strings.ReplaceAll(raw, "fixture-uuid", "other-uuid")
							raw = strings.Replace(raw, `"uuid":"other-uuid"`, `"uuid":"fixture-uuid"`, 1)
						}
					}
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				backup := filepath.Join(t.TempDir(), "private")
				if failure == "save-fail" {
					if err := os.WriteFile(backup, []byte("occupied"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				opts := &mcp.ClientOptions{ElicitationHandler: vmApprove}
				if failure == "missing-picker" {
					opts = nil
				}
				session := vmConnect(t, transport, backend.URL, backup, opts)
				name := "set_vm_cpu_pinning"
				args := json.RawMessage(tc.Args)
				if failure == "xml-uuid" {
					name = "update_vm_xml"
					args = json.RawMessage(`{"vm_name":"guest","machine_name":"host","vm_xml":"<domain><name>guest</name><uuid>different</uuid></domain>"}`)
				}
				if failure == "host-mismatch" {
					name = "update_vm_cpu_xml"
					args = json.RawMessage(`{"vm_name":"guest","machine_name":"different","cpu_xml":"<cpu/>"}`)
				}
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
				if writes.Load() != 0 {
					t.Fatal("unsafe mutation occurred")
				}
				if err == nil && out != nil && !out.IsError && !out.NeedsInput() {
					t.Fatalf("failure reported success: %#v", out)
				}
			})
		}
	}
}
func TestVMApprovalTokensThroughBothTransports(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			tc := vmPinningCase()
			var writes atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes.Add(1)
					return
				}
				raw, _ := vmSnapshotFixture(strings.TrimPrefix(r.URL.Path, "/prefix/api"), tc)
				fmt.Fprint(w, raw)
			}))
			defer backend.Close()
			session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
			args := json.RawMessage(tc.Args)
			start := func() *mcp.CallToolResult {
				t.Helper()
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.Name, Arguments: args})
				if err != nil || !out.NeedsInput() || out.StructuredContent != nil || len(out.Content) != 0 {
					t.Fatalf("pending falsely completed %#v %v", out, err)
				}
				return out
			}
			approve := func(token string, input any) *mcp.CallToolResult {
				t.Helper()
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.Name, Arguments: input, RequestState: token, InputResponses: mcp.InputResponseMap{"vm_approval": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}}})
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			first := start()
			changed := strings.Replace(tc.Args, `"range_end":1`, `"range_end":2`, 1)
			if !approve(first.RequestState, json.RawMessage(changed)).IsError || writes.Load() != 0 {
				t.Fatal("changed arguments bypassed picker")
			}
			if !approve("forged", args).IsError || writes.Load() != 0 {
				t.Fatal("forged approval bypassed picker")
			}
			second := start()
			if approve(second.RequestState, args).IsError || writes.Load() != 1 {
				t.Fatal("approved write failed")
			}
			if !approve(second.RequestState, args).IsError || writes.Load() != 1 {
				t.Fatal("approval replay mutated")
			}
		})
	}
}
