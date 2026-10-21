package hyperhivemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type vmReadCase struct {
	Name, Path, Query, Body, Response string
	Args                              map[string]any
}

var vmReadCases = []vmReadCase{
	{"list_vms", "/virsh/getallvms", "", "", `[{"name":"guest","machineName":"host","state":5}]`, map[string]any{}},
	{"get_vm", "/virsh/getvmbyname/guest", "", "", `{"name":"guest","machineName":"host","state":5}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_xml", "/virsh/vmxml/guest", "machine_name=host", "", `{"vm_xml":"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>","machine_name":"host"}`, map[string]any{"vm_name": "guest", "machine_name": "host"}},
	{"get_vm_cpu_xml", "/virsh/cpuxml/guest", "", "{\"machine_name\":\"host\"}", `{"cpu_xml":"<cpu/>"}`, map[string]any{"vm_name": "guest", "machine_name": "host"}},
	{"list_vm_attached_disks", "/virsh/vm_disk/guest", "", "", `[]`, map[string]any{"vm_name": "guest"}},
	{"get_vm_video", "/virsh/novncvideo/guest", "", "", `{}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_ballooning", "/virsh/ballooning/guest", "", "", `{}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_hugepages", "/virsh/hugepages/guest", "", "", `{}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_kvm", "/virsh/kvm/guest", "", "", `{"vm_name":"guest","hidden":false}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_hyperv", "/virsh/hyperv/guest", "", "", `{"vm_name":"guest","hyperv":false}`, map[string]any{"vm_name": "guest"}},
	{"get_vm_cpu_pinning", "/virsh/cpupinning/guest", "", "", `{"hasPinning":false,"pins":[],"rangeStart":0,"rangeEnd":0,"hyperThreading":false,"socketId":0,"emulatorCpuset":""}`, map[string]any{"vm_name": "guest"}},
	{"list_vm_machine_types", "/virsh/machine_types/host", "", "", `{"machine_types":["pc-q35"]}`, map[string]any{"machine_name": "host"}},
	{"get_vm_cpu_topology", "/virsh/cputopology/host", "", "", `{"sockets":[]}`, map[string]any{"machine_name": "host"}},
	{"get_vm_tuned_profiles", "/virsh/tunedadm/host", "", "", `{"profiles":[],"currentActiveProfile":"balanced"}`, map[string]any{"machine_name": "host"}},
	{"get_vm_irqbalance", "/virsh/irqbalance/host", "", "", `{"enabled":false,"active":false,"unitName":"irqbalance.service"}`, map[string]any{"machine_name": "host"}},
	{"get_vm_core_isolation", "/virsh/coreisolation/host", "", "", `{"enabled":false,"rebootRequired":false,"message":"","isolcpus":"","nohzFull":"","rcuNocbs":"","activeIsolcpus":"","configuredCpus":[],"activeCpus":[],"sockets":[]}`, map[string]any{"machine_name": "host"}},
	{"get_vm_host_hugepages", "/virsh/hosthugepages/host", "", "", `{"enabled":false,"rebootRequired":false,"message":"","pageSize":"","pageCount":0,"activePageSize":"","activePageCount":0,"defaultHugepagesz":"","hugepagesz":"","hugepages":"","activeDefaultHugepagesz":"","activeHugepagesz":"","activeHugepages":""}`, map[string]any{"machine_name": "host"}},
	{"list_vm_backups", "/virsh/backups", "", "", `null`, map[string]any{}},
	{"list_vm_automatic_backups", "/virsh/autobak", "", "", `null`, map[string]any{}},
	{"list_vm_xml_templates", "/virsh/xmltemplates/", "", "", `[]`, map[string]any{}},
	{"get_vm_xml_template", "/virsh/xmltemplates/7", "", "", `{"id":7,"name":"template","description":"","xml":"<domain/>","created_at":"","updated_at":""}`, map[string]any{"id": 7}},
	{"list_vm_disks", "/vm-disk/list", "", "", `[]`, map[string]any{}},
	{"get_vm_cpu_disable_features", "/virsh/getcpudisablefeatures", "slavesnames=host&slavesnames=other", "", `"<cpu/>"`, map[string]any{"slavesnames": []string{"host", "other"}}},
}

func vmConnect(t *testing.T, transport, backend, backup string, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c := mcp.NewClient(&mcp.Implementation{Name: "vm-contract"}, opts)
	var session *mcp.ClientSession
	var err error
	var connectOptions *mcp.ClientSessionOptions
	if strings.HasPrefix(transport, "legacy-") {
		connectOptions = &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"}
	}
	if strings.Contains(transport, "http") {
		endpoint := httptest.NewServer(NewHTTPHandler(Config{BaseURL: backend + "/prefix/api", MCPToken: "vm-fixture", VMBackupDir: backup, SafetyReviewDir: backup + "-reviews"}))
		t.Cleanup(endpoint.Close)
		session, err = c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{"vm-fixture"}}}, connectOptions)
	} else {
		cmd := exec.CommandContext(ctx, nginxBinary, "serve")
		cmd.Env = append(os.Environ(), "HYPERHIVE_API_URL="+backend+"/prefix/api", "MCP_TOKEN=vm-fixture", "MCP_VM_BACKUP_DIR="+backup, "MCP_REVIEW_DIR="+backup+"-reviews")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		session, err = c.Connect(ctx, &mcp.CommandTransport{Command: cmd}, connectOptions)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func TestVMReadContracts(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			for _, tc := range vmReadCases {
				t.Run(tc.Name, func(t *testing.T) {
					var mu sync.Mutex
					calls, status := 0, 200
					response := tc.Response
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						calls++
						if r.Method != "GET" || r.URL.Path != "/prefix/api"+tc.Path || r.URL.RawQuery != tc.Query || r.Header.Get("Authorization") != "Bearer vm-fixture" {
							t.Errorf("unexpected wire %s %s", r.Method, r.URL)
						}
						b, _ := io.ReadAll(r.Body)
						if string(b) != tc.Body {
							t.Errorf("body=%s want %s", b, tc.Body)
						}
						w.WriteHeader(status)
						fmt.Fprint(w, response)
					}))
					defer backend.Close()
					session := vmConnect(t, transport, backend.URL, t.TempDir(), nil)
					ctx := context.Background()
					listed, err := session.ListTools(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					var tool *mcp.Tool
					for _, candidate := range listed.Tools {
						if candidate.Name == tc.Name {
							tool = candidate
						}
					}
					if tool == nil || tool.Meta["hyperhive/risk"] != "low" || !tool.Annotations.ReadOnlyHint || tool.OutputSchema == nil {
						t.Fatalf("missing/unsafe discovery: %#v", tool)
					}
					call := func(args any) *mcp.CallToolResult {
						t.Helper()
						result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: args})
						if err != nil {
							t.Fatal(err)
						}
						return result
					}
					result := call(tc.Args)
					if result.IsError {
						t.Fatalf("success failed: %#v", result)
					}
					raw, _ := json.Marshal(result.StructuredContent)
					var out struct {
						Result json.RawMessage `json:"result"`
					}
					if json.Unmarshal(raw, &out) != nil {
						t.Fatal("invalid output")
					}
					if !bytes.Equal(bytes.TrimSpace(out.Result), bytes.TrimSpace([]byte(tc.Response))) {
						var a, b any
						json.Unmarshal(out.Result, &a)
						json.Unmarshal([]byte(tc.Response), &b)
						aa, _ := json.Marshal(a)
						bb, _ := json.Marshal(b)
						if string(aa) != string(bb) {
							t.Fatalf("response changed: %s", raw)
						}
					}
					mu.Lock()
					before := calls
					mu.Unlock()
					for _, bad := range []any{json.RawMessage(`null`), json.RawMessage(`[]`), map[string]any{"unknown": true}} {
						if !call(bad).IsError {
							t.Fatalf("accepted invalid args %s", bad)
						}
					}
					mu.Lock()
					if calls != before {
						t.Error("invalid args reached backend")
					}
					status = 500
					response = "PRIVATE backend secret"
					mu.Unlock()
					result = call(tc.Args)
					encoded, _ := json.Marshal(result)
					if !result.IsError || strings.Contains(string(encoded), "PRIVATE") {
						t.Fatalf("unsafe backend error %s", encoded)
					}
					mu.Lock()
					status = 200
					response = `true`
					mu.Unlock()
					if !call(tc.Args).IsError {
						t.Error("accepted wrong response shape")
					}
				})
			}
		})
	}
}
