package hyperhivemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReviewVMEditDiskRecovery(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		for _, state := range []string{"missing-size", "zero-size", "fraction-size", "missing-path", "wrong-path", "missing-disk", "valid", "cpu-only"} {
			t.Run(transport+"/"+state, func(t *testing.T) {
				vm := map[string]any{"name": "guest", "machineName": "host", "diskSizeGB": 10, "diskPath": "/fixture/boot.qcow2"}
				switch state {
				case "missing-size":
					delete(vm, "diskSizeGB")
				case "zero-size":
					vm["diskSizeGB"] = 0
				case "fraction-size":
					vm["diskSizeGB"] = 10.5
				case "missing-path":
					delete(vm, "diskPath")
				case "wrong-path":
					vm["diskPath"] = "/fixture/other.qcow2"
				case "cpu-only":
					delete(vm, "diskSizeGB")
					delete(vm, "diskPath")
				}
				domain := `<domain><name>guest</name><uuid>fixture-uuid</uuid><devices><disk device="cdrom"><source file="/fixture/os.iso"/></disk><disk device="disk"><source file="/fixture/boot.qcow2"/></disk></devices></domain>`
				if state == "missing-disk" || state == "cpu-only" {
					domain = `<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>`
				}
				var writes atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" {
						writes.Add(1)
						return
					}
					var response any
					switch strings.TrimPrefix(r.URL.Path, "/prefix/api") {
					case "/virsh/getvmbyname/guest":
						response = vm
					case "/virsh/vmxml/guest":
						response = map[string]any{"vm_xml": domain, "machine_name": "host"}
					case "/virsh/snapshot/guest":
						response = map[string]any{"name": "guest", "uuid": "fixture-uuid", "machine_name": "host", "active": false, "inactive_xml": domain, "current_xml": ""}
					default:
						t.Errorf("unexpected request %s", r.URL)
						http.Error(w, "unexpected", 404)
						return
					}
					json.NewEncoder(w).Encode(response)
				}))
				defer backend.Close()
				private := filepath.Join(t.TempDir(), "private")
				session := vmConnect(t, transport, backend.URL, private, &mcp.ClientOptions{ElicitationHandler: vmApprove})
				args := map[string]any{"vm_name": "guest", "disk_sizeGB": 20}
				if state == "cpu-only" {
					args = map[string]any{"vm_name": "guest", "vcpu": 2}
				}
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "edit_vm", Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				wantWrite := state == "valid" || state == "cpu-only"
				if (writes.Load() == 1) != wantWrite || out.IsError == wantWrite {
					t.Fatalf("writes=%d wantWrite=%t result=%+v", writes.Load(), wantWrite, out)
				}
				records, _ := filepath.Glob(filepath.Join(private, "change-*", "before.json"))
				if !wantWrite && len(records) != 0 {
					t.Fatal("invalid recovery was persisted")
				}
				if state == "valid" {
					if len(records) != 1 {
						t.Fatal("missing recovery")
					}
					data, e := os.ReadFile(records[0])
					if e != nil {
						t.Fatal(e)
					}
					if !strings.Contains(string(data), `"diskSizeGB": 10`) && !strings.Contains(string(data), `"diskSizeGB":10`) {
						t.Fatal(fmt.Sprintf("old size absent: %s", data))
					}
					if !strings.Contains(string(data), "/fixture/boot.qcow2") {
						t.Fatal("old disk path absent")
					}
				}
			})
		}
	}
}
