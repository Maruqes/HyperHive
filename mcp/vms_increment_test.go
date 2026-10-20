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

// These wire fixtures follow master/api/virsh.go, including explicit false
// flags and the schedule handlers' unconditional Enabled=true behavior.
func init() {
	var base map[string]string
	for _, tc := range vmWriteCases {
		if tc.Name == "start_vm" {
			base = tc.Snapshots
		}
	}
	clone := func() map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		return m
	}
	vm := `{"name":"guest","machineName":"host","state":5,"autoStart":true,"isLive":true,"VNCPassword":"old-private-password","DefinedCPUS":2,"DefinedRam":1024,"diskSizeGB":10,"MachineType":"pc-q35","network":"default"}`
	for _, tc := range []vmWriteCase{
		{Name: "set_vm_autostart", Method: "POST", Path: "/virsh/autostart/guest", Args: `{"vm_name":"guest","auto_start":false}`, Body: `{"auto_start":false}`, Risk: "medium"},
		{Name: "set_vm_live", Method: "POST", Path: "/virsh/vmlive/guest", Args: `{"vm_name":"guest","enable":false}`, Body: `{"enable":false}`, Risk: "high"},
	} {
		tc.Snapshots = clone()
		tc.Snapshots["vm"] = vm
		vmWriteCases = append(vmWriteCases, tc)
	}
	share := `{"Id":2,"MachineName":"host","FolderPath":"/fixture","Source":"fixture:/data","Target":"/fixture","Name":"share","HostNormalMount":false}`
	schedule := `{"Id":7,"VmName":"guest","FrequencyDays":2,"MinTime":"01:00","MaxTime":"03:00","NfsMountId":2,"MaxBackupsRetain":3,"Enabled":false,"LastBackupTime":null}`
	settings := `{"vm_name":"guest","frequency_days":2,"min_time":"23:30","max_time":"01:00","nfs_mount_id":2,"max_backups_retain":3}`
	for _, tc := range []vmWriteCase{
		{Name: "create_vm_automatic_backup", Method: "POST", Path: "/virsh/autobak", Args: settings, Body: settings, Risk: "very high"},
		{Name: "update_vm_automatic_backup", Method: "PUT", Path: "/virsh/autobak/7", Args: `{"id":7,"vm_name":"guest","frequency_days":2,"min_time":"23:30","max_time":"01:00","nfs_mount_id":2,"max_backups_retain":3}`, Body: settings, Risk: "very high"},
		{Name: "enable_vm_automatic_backup", Method: "PATCH", Path: "/virsh/autopak/enable/7", Args: `{"id":7}`, Risk: "very high"},
	} {
		tc.Snapshots = clone()
		tc.Snapshots["nfs_share"] = share
		tc.Snapshots["schedules"] = `{"response":[` + schedule + `]}`
		if tc.Name != "create_vm_automatic_backup" {
			tc.Snapshots["schedule"] = schedule
		}
		vmWriteCases = append(vmWriteCases, tc)
	}
	create := `{"machine_name":"host","name":"new-guest","memory":2048,"vcpu":2,"disk_sizeGB":10,"template_id":0,"iso_id":8,"nfs_share_id":2,"network":"default","VNC_password":"new-private-password","cpu_xml":"","live":false,"auto_start":false,"is_windows":false}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "create_vm", Method: "POST", Path: "/virsh/createvm", Args: create, Body: create, Response: "VM created successfully", Risk: "high", Snapshots: map[string]string{"nfs_share": share, "vms": `{"response":[{"name":"guest","machineName":"host"}]}`, "iso": `{"Id":8,"MachineName":"host","FilePath":"/fixture/os.iso","Name":"os.iso","available_on_slaves":{"host":true}}`}})
	// Exercise both boolean values and the live/template creation branch over all
	// transport/protocol modes through the same independently specified harness.
	for _, name := range []string{"set_vm_autostart", "set_vm_live"} {
		for _, tc := range vmWriteCases {
			if tc.Name == name {
				tc.Args = strings.Replace(tc.Args, "false", "true", 1)
				tc.Body = strings.Replace(tc.Body, "false", "true", 1)
				vmWriteCases = append(vmWriteCases, tc)
				break
			}
		}
	}
	template := `{"id":7,"name":"template","description":"fixture","xml":"<domain/>","created_at":"","updated_at":""}`
	createTemplate := `{"machine_name":"host","name":"new-guest","memory":0,"vcpu":0,"disk_sizeGB":10,"template_id":7,"iso_id":0,"nfs_share_id":2,"network":"","VNC_password":"","cpu_xml":"","live":true,"auto_start":true,"is_windows":true}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "create_vm", Method: "POST", Path: "/virsh/createvm", Args: createTemplate, Body: createTemplate, Response: "VM created successfully", Risk: "high", Snapshots: map[string]string{"nfs_share": share, "vms": `{"response":[{"name":"guest","machineName":"host"}]}`, "template": template}})

}

func TestVMIncrementSnapshotGuards(t *testing.T) {
	cases := []struct{ Name, Tool, Path, Old, New string }{
		{"autostart-missing", "set_vm_autostart", "/virsh/getvmbyname/guest", `"autoStart":true,`, ""},
		{"autostart-null", "set_vm_autostart", "/virsh/getvmbyname/guest", `"autoStart":true`, `"autoStart":null`},
		{"live-wrong-type", "set_vm_live", "/virsh/getvmbyname/guest", `"isLive":true`, `"isLive":"true"`},
		{"duplicate-name", "create_vm", "/virsh/getallvms", "guest", "new-guest"},
		{"missing-iso", "create_vm", "/isos/", `"Id":8`, `"Id":9`},
		{"missing-nfs", "create_vm", "/nfs/list", `"Id":2`, `"Id":9`},
		{"schedule-not-found", "update_vm_automatic_backup", "/virsh/autobak", `"Id":7`, `"Id":9`},
		{"schedule-enabled", "enable_vm_automatic_backup", "/virsh/autobak", `"Enabled":false`, `"Enabled":true`},
		{"schedule-invalid-retention", "enable_vm_automatic_backup", "/virsh/autobak", `"MaxBackupsRetain":3`, `"MaxBackupsRetain":null`},
	}
	for _, transport := range []string{"http", "stdio"} {
		for _, guard := range cases {
			t.Run(transport+"/"+guard.Name, func(t *testing.T) {
				var tc vmWriteCase
				for _, c := range vmWriteCases {
					if c.Name == guard.Tool {
						tc = c
						break
					}
				}
				var writes atomic.Int32
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						writes.Add(1)
						return
					}
					path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
					raw, _ := vmSnapshotFixture(path, tc)
					if path == guard.Path {
						raw = strings.Replace(raw, guard.Old, guard.New, 1)
					}
					fmt.Fprint(w, raw)
				}))
				defer backend.Close()
				session := vmConnect(t, transport, backend.URL, filepath.Join(t.TempDir(), "private"), &mcp.ClientOptions{ElicitationHandler: vmApprove})
				out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.Name, Arguments: json.RawMessage(tc.Args)})
				if err == nil && out != nil && !out.IsError {
					t.Fatalf("invalid snapshot accepted: %#v", out)
				}
				if writes.Load() != 0 {
					t.Fatal("mutation despite invalid prior state")
				}
			})
		}
	}
}
