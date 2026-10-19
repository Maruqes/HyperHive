package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var vmBootSnapshotFixture string

func init() {
	raw, _ := json.Marshal(map[string]any{"machine_name": "host", "grubby_info": "index=0\nkernel=\"/boot/vmlinuz-a\"\nargs=\"ro isolcpus=1\"\nindex=1\nkernel=\"/boot/vmlinuz-b\"\nargs=\"ro nohz_full=2\"\n", "current_cmdline": "ro quiet\n", "default_kernel": "/boot/vmlinuz-a", "entries": []map[string]any{{"index": 0, "kernel": "/boot/vmlinuz-a", "args": "ro isolcpus=1"}, {"index": 1, "kernel": "/boot/vmlinuz-b", "args": "ro nohz_full=2"}}})
	vmBootSnapshotFixture = string(raw)
	read := func(name string) string {
		for _, tc := range vmReadCases {
			if tc.Name == name {
				return tc.Response
			}
		}
		panic("missing host read fixture")
	}
	for _, method := range []string{"POST", "PUT"} {
		name := "set_vm_host_core_isolation"
		hugeName := "set_vm_host_hugepages"
		if method == "PUT" {
			name = "replace_vm_host_core_isolation"
			hugeName = "replace_vm_host_hugepages"
		}
		vmWriteCases = append(vmWriteCases, vmWriteCase{Name: name, Method: method, Path: "/virsh/coreisolation/host", Args: `{"machine_name":"host","sockets":[{"socket_id":0,"core_indices":[0,1]}]}`, Body: `{"sockets":[{"socket_id":0,"core_indices":[0,1]}]}`, Response: read("get_vm_core_isolation"), Risk: "very high", Snapshots: map[string]string{"host_boot": vmBootSnapshotFixture, "host_setting": read("get_vm_core_isolation")}}, vmWriteCase{Name: hugeName, Method: method, Path: "/virsh/hosthugepages/host", Args: `{"machine_name":"host","page_size":"2M","page_count":16}`, Body: `{"page_size":"2M","page_count":16}`, Response: read("get_vm_host_hugepages"), Risk: "very high", Snapshots: map[string]string{"host_boot": vmBootSnapshotFixture, "host_setting": read("get_vm_host_hugepages")}})
	}
}
func TestVMHostBootSnapshotFailuresCannotWrite(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		for _, kind := range []string{"missing", "truncated", "fraction", "mismatch", "missing-args"} {
			t.Run(transport+"/"+kind, func(t *testing.T) {
				var writes atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						return
					}
					raw := vmBootSnapshotFixture
					switch kind {
					case "missing":
						raw = `{}`
					case "truncated":
						raw = strings.Replace(raw, `nohz_full=2\"\n`, `nohz_full=2`, 1)
					case "fraction":
						raw = strings.Replace(raw, `"index":0`, `"index":0.5`, 1)
					case "mismatch":
						raw = strings.Replace(raw, `"args":"ro isolcpus=1"`, `"args":"other"`, 1)
					case "missing-args":
						raw = strings.Replace(raw, `"args":"ro isolcpus=1"`, `"other":"ro isolcpus=1"`, 1)
					}
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{ElicitationHandler: vmApprove})
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "set_vm_host_hugepages", Arguments: map[string]any{"machine_name": "host", "page_size": "2M", "page_count": 16}})
				if err == nil && !out.IsError {
					t.Fatal("invalid host snapshot accepted")
				}
				if writes.Load() != 0 {
					t.Fatal("host changed after bad snapshot")
				}
			})
		}
	}
}
