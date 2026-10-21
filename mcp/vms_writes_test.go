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
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type vmWriteCase struct {
	Name, Method, Path, Args, Body, Response, Risk string
	Snapshots                                      map[string]string
}

var vmWriteCases = []vmWriteCase{
	{"start_vm", "POST", "/virsh/startvm/guest", "{\"vm_name\":\"guest\"}", "", "", "medium", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"shutdown_vm", "POST", "/virsh/shutdownvm/guest", "{\"vm_name\":\"guest\"}", "", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"pause_vm", "POST", "/virsh/pausevm/guest", "{\"vm_name\":\"guest\"}", "", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"resume_vm", "POST", "/virsh/resumevm/guest", "{\"vm_name\":\"guest\"}", "", "", "medium", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"force_shutdown_vm", "POST", "/virsh/forceshutdownvm/guest", "{\"vm_name\":\"guest\"}", "", "", "very high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"restart_vm", "POST", "/virsh/restartvm/guest", "{\"vm_name\":\"guest\"}", "", "", "very high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"set_vm_cpu_pinning", "POST", "/virsh/cpupinning/guest", "{\"vm_name\":\"guest\",\"range_start\":0,\"range_end\":1,\"hyper_threading\":false,\"socket_id\":0}", "{\"range_start\":0,\"range_end\":1,\"hyper_threading\":false,\"socket_id\":0}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{\"hasPinning\":false,\"pins\":[],\"rangeStart\":0,\"rangeEnd\":0,\"hyperThreading\":false,\"socketId\":0,\"emulatorCpuset\":\"\"}"}},
	{"update_vm_cpu_xml", "POST", "/virsh/updatecpuxml/guest", "{\"vm_name\":\"guest\",\"machine_name\":\"host\",\"cpu_xml\":\"<cpu/>\"}", "{\"machine_name\":\"host\",\"cpu_xml\":\"<cpu/>\"}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "cpu_xml": "{\"cpu_xml\":\"<cpu/>\"}"}},
	{"set_vm_ballooning", "POST", "/virsh/ballooning/guest", "{\"vm_name\":\"guest\",\"enable\":false}", "{\"enable\":false}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{}"}},
	{"set_vm_hugepages", "POST", "/virsh/hugepages/guest", "{\"vm_name\":\"guest\",\"enable\":false}", "{\"enable\":false}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{}"}},
	{"set_vm_kvm", "POST", "/virsh/kvm/guest", "{\"vm_name\":\"guest\",\"hidden\":false}", "{\"hidden\":false}", "{\"vm_name\":\"guest\",\"hidden\":false}", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{\"vm_name\":\"guest\",\"hidden\":false}"}},
	{"set_vm_hyperv", "POST", "/virsh/hyperv/guest", "{\"vm_name\":\"guest\",\"hyperv\":false}", "{\"hyperv\":false}", "{\"vm_name\":\"guest\",\"hyperv\":false}", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{\"vm_name\":\"guest\",\"hyperv\":false}"}},
	{"set_vm_machine_type", "POST", "/virsh/machine_type/guest", "{\"vm_name\":\"guest\",\"machine_type\":\"pc-q35\"}", "{\"machine_type\":\"pc-q35\"}", "{\"vm_name\":\"guest\",\"machine_type\":\"pc-q35\"}", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"set_vm_network", "POST", "/virsh/change_vm_network/guest", "{\"vm_name\":\"guest\",\"new_network\":\"512rede\"}", "{\"new_network\":\"512rede\"}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"set_vm_vnc_password", "POST", "/virsh/change_vnc_password/guest", "{\"vm_name\":\"guest\",\"new_password\":\"new-private-password\"}", "{\"new_password\":\"new-private-password\"}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"add_vm_video", "POST", "/virsh/novncvideo/add/guest", "{\"vm_name\":\"guest\"}", "", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}", "setting": "{}"}},
	{"edit_vm", "POST", "/virsh/editvm/guest", "{\"vm_name\":\"guest\",\"memory\":2048,\"vcpu\":0,\"disk_sizeGB\":20}", "{\"memory\":2048,\"disk_sizeGB\":20}", "", "high", map[string]string{"vm": "{\"name\":\"guest\",\"machineName\":\"host\",\"state\":5,\"VNCPassword\":\"old-private-password\",\"DefinedCPUS\":2,\"DefinedRam\":1024,\"diskSizeGB\":10,\"diskPath\":\"/fixture/boot.qcow2\",\"MachineType\":\"pc-q35\",\"network\":\"default\"}", "domain_xml": "{\"vm_xml\":\"<domain><name>guest</name><uuid>fixture-uuid</uuid></domain>\",\"machine_name\":\"host\"}"}},
	{"set_vm_tuned_profile", "POST", "/virsh/tunedadm/host", "{\"machine_name\":\"host\",\"profile\":\"balanced\"}", "{\"profile\":\"balanced\"}", "{\"ok\":true,\"message\":\"applied\",\"currentActiveProfile\":\"balanced\"}", "high", map[string]string{"host_setting": "{\"profiles\":[],\"currentActiveProfile\":\"balanced\"}"}},
	{"set_vm_irqbalance", "POST", "/virsh/irqbalance/host", "{\"machine_name\":\"host\",\"enabled\":false}", "{\"enabled\":false}", "{\"ok\":true,\"message\":\"applied\",\"enabled\":false,\"active\":false,\"unitName\":\"irqbalance.service\"}", "high", map[string]string{"host_setting": "{\"enabled\":false,\"active\":false,\"unitName\":\"irqbalance.service\"}"}},
	{"create_vm_xml_template", "POST", "/virsh/xmltemplates/", "{\"name\":\"new-template\",\"description\":\"new\",\"xml\":\"<domain><name>$name</name><devices><disk><driver type=\\\"qcow2\\\"/><source file=\\\"$qcow2\\\"/></disk></devices></domain>\"}", "{\"name\":\"new-template\",\"description\":\"new\",\"xml\":\"<domain><name>$name</name><devices><disk><driver type=\\\"qcow2\\\"/><source file=\\\"$qcow2\\\"/></disk></devices></domain>\"}", "{\"id\":7,\"name\":\"template\",\"description\":\"old\",\"xml\":\"<domain/>\",\"created_at\":\"\",\"updated_at\":\"\"}", "medium", map[string]string{"templates": "[]"}},
	{"update_vm_xml_template", "PUT", "/virsh/xmltemplates/7", "{\"id\":7,\"name\":\"new-template\",\"description\":\"new\",\"xml\":\"<domain><name>$name</name><devices><disk><driver type=\\\"qcow2\\\"/><source file=\\\"$qcow2\\\"/></disk></devices></domain>\"}", "{\"name\":\"new-template\",\"description\":\"new\",\"xml\":\"<domain><name>$name</name><devices><disk><driver type=\\\"qcow2\\\"/><source file=\\\"$qcow2\\\"/></disk></devices></domain>\"}", "{\"id\":7,\"name\":\"template\",\"description\":\"old\",\"xml\":\"<domain/>\",\"created_at\":\"\",\"updated_at\":\"\"}", "high", map[string]string{"templates": "{\"id\":7,\"name\":\"template\",\"description\":\"old\",\"xml\":\"<domain/>\",\"created_at\":\"\",\"updated_at\":\"\"}"}},
	{"grow_vm_disk", "POST", "/vm-disk/7/grow", "{\"id\":7,\"size_gb\":20}", "{\"size_gb\":20}", "{\"id\":7,\"name\":\"disk\",\"nfs_id\":2,\"disk_path\":\"/fixture/disk.qcow2\",\"folder_path\":\"/fixture\",\"format\":\"qcow2\",\"size_gb\":10,\"occupied_gb\":1,\"occupied_bytes\":9007199254740993,\"attached_vm_name\":\"\"}", "high", map[string]string{"disk": "{\"id\":7,\"name\":\"disk\",\"nfs_id\":2,\"disk_path\":\"/fixture/disk.qcow2\",\"folder_path\":\"/fixture\",\"format\":\"qcow2\",\"size_gb\":10,\"occupied_gb\":1,\"occupied_bytes\":9007199254740993,\"attached_vm_name\":\"\"}"}},
}

func vmSnapshotFixture(path string, tc vmWriteCase) (string, bool) {
	switch path {
	case "/virsh/hostsnapshot/host":
		return vmBootSnapshotFixture, true
	case "/isos/":
		return `[{"Id":8,"MachineName":"host","FilePath":"/fixture/os.iso","Name":"os.iso","available_on_slaves":{"host":true}}]`, true
	case "/nfs/list":
		return `[{"NfsShare":{"Id":2,"MachineName":"host","FolderPath":"/fixture","Source":"fixture:/data","Target":"/fixture","Name":"share","HostNormalMount":false},"Status":{"working":true}}]`, true
	case "/virsh/getallvms":
		return `[{"name":"guest","machineName":"host"}]`, true
	case "/virsh/backups":
		return `null`, true
	case "/virsh/autobak":
		if tc.Snapshots["schedules"] != "" {
			return `[{"Id":7,"VmName":"guest","FrequencyDays":2,"MinTime":"01:00","MaxTime":"03:00","NfsMountId":2,"MaxBackupsRetain":3,"Enabled":false,"LastBackupTime":null}]`, true
		}
		return "[" + tc.Snapshots["schedule"] + "]", true
	case "/virsh/vm_disk/guest":
		return tc.Snapshots["attached_disks"], true
	case "/virsh/snapshot/guest":
		return vmSecureXMLFixture, true
	case "/virsh/getvmbyname/guest":
		return tc.Snapshots["vm"], true
	case "/virsh/vmxml/guest":
		return tc.Snapshots["domain_xml"], true
	case "/virsh/xmltemplates/":
		return tc.Snapshots["templates"], true
	case "/virsh/xmltemplates/7":
		if raw := tc.Snapshots["template"]; raw != "" {
			return raw, true
		}
		return tc.Snapshots["templates"], true
	case "/vm-disk/list":
		return "[" + tc.Snapshots["disk"] + "]", true
	case "/virsh/cpuxml/guest":
		return tc.Snapshots["cpu_xml"], true
	}
	if value := tc.Snapshots["setting"]; value != "" {
		return value, true
	}
	if value := tc.Snapshots["host_setting"]; value != "" {
		return value, true
	}
	return "", false
}
func TestVMWriteContracts(t *testing.T) {
	for _, transport := range []string{"http", "stdio", "legacy-http", "legacy-stdio"} {
		t.Run(transport, func(t *testing.T) {
			for _, tc := range vmWriteCases {
				if _, ok := tc.Snapshots["vm"]; ok {
					tc.Snapshots["secure_domain"] = vmSecureXMLFixture
				}
				t.Run(tc.Name, func(t *testing.T) {
					var mu sync.Mutex
					writes, reads, prompts := 0, 0, 0
					mode := "success"
					backup := filepath.Join(t.TempDir(), "private")
					var lastBackup string
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						if r.Header.Get("Authorization") != "Bearer vm-fixture" {
							t.Error("wrong authentication")
						}
						path := strings.TrimPrefix(r.URL.Path, "/prefix/api")
						if r.Method == "GET" {
							reads++
							if mode == "read-fail" {
								http.Error(w, "PRIVATE snapshot", 500)
								return
							}
							raw, ok := vmSnapshotFixture(path, tc)
							if !ok || raw == "" {
								t.Errorf("unexpected snapshot %s", r.URL)
							}
							fmt.Fprint(w, raw)
							return
						}
						writes++
						if r.Method != tc.Method || path != tc.Path {
							t.Errorf("write wire %s %s", r.Method, r.URL)
						}
						raw, _ := io.ReadAll(r.Body)
						var got, want any
						if len(raw) > 0 {
							json.Unmarshal(raw, &got)
						}
						if tc.Body != "" {
							json.Unmarshal([]byte(tc.Body), &want)
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body %s want %s", raw, tc.Body)
						}
						files, _ := filepath.Glob(filepath.Join(backup, "change-*", "before.json"))
						if len(files) == 0 {
							t.Error("write before persisted backup")
						} else {
							lastBackup = files[len(files)-1]
							data, err := os.ReadFile(lastBackup)
							if err != nil {
								t.Fatal(err)
							}
							var saved struct {
								Tool      string                     `json:"tool"`
								Snapshots map[string]json.RawMessage `json:"snapshots"`
							}
							if json.Unmarshal(data, &saved) != nil || saved.Tool != tc.Name {
								t.Error("invalid backup")
							}
							for k, v := range tc.Snapshots {
								var a, b bytes.Buffer
								json.Compact(&a, saved.Snapshots[k])
								json.Compact(&b, []byte(v))
								var av, bv any
								ad := json.NewDecoder(&a)
								ad.UseNumber()
								bd := json.NewDecoder(&b)
								bd.UseNumber()
								ad.Decode(&av)
								bd.Decode(&bv)
								if !reflect.DeepEqual(av, bv) {
									t.Errorf("snapshot %s changed: %s want %s", k, a.String(), b.String())
								}
							}
						}
						if mode == "write-fail" {
							http.Error(w, "PRIVATE backend", 500)
							return
						}
						if tc.Name == "create_vm" {
							w.WriteHeader(http.StatusCreated)
						}
						fmt.Fprint(w, tc.Response)
					}))
					defer backend.Close()
					decision := "approve"
					opts := &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
						mu.Lock()
						prompts++
						if !strings.Contains(req.Params.Message, "Risk: "+tc.Risk) || !strings.Contains(req.Params.Message, tc.Name) {
							t.Error("picker lacks operation/risk")
						}
						if strings.Contains(req.Params.Message, "new-private-password") {
							t.Error("picker leaked password")
						}
						mu.Unlock()
						if decision == "decline" {
							return &mcp.ElicitResult{Action: "decline"}, nil
						}
						if decision == "cancel" {
							return &mcp.ElicitResult{Action: "cancel"}, nil
						}
						return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": "approve"}}, nil
					}}
					session := vmConnect(t, transport, backend.URL, backup, opts)
					ctx := context.Background()
					call := func(args any) *mcp.CallToolResult {
						t.Helper()
						out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.Name, Arguments: args})
						if err != nil {
							t.Fatal(err)
						}
						return out
					}
					args := json.RawMessage(tc.Args)
					choices := []string{"decline", "cancel"}
					if tc.Risk == "medium" {
						choices = nil
					}
					for _, choice := range choices {
						decision = choice
						out := call(args)
						if !out.IsError {
							t.Errorf("%s accepted", choice)
						}
					}
					mu.Lock()
					if writes != 0 || reads != 0 {
						t.Error("decline/cancel touched backend")
					}
					mu.Unlock()
					decision = "approve"
					for _, bad := range []any{json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{}`), map[string]any{"unknown": true}} {
						if !call(bad).IsError {
							t.Errorf("accepted invalid input %v", bad)
						}
					}
					mu.Lock()
					beforeWrites := writes
					mode = "read-fail"
					mu.Unlock()
					if !call(args).IsError {
						t.Error("read failure passed")
					}
					mu.Lock()
					if writes != beforeWrites {
						t.Error("snapshot failure mutated")
					}
					mode = "success"
					mu.Unlock()
					out := call(args)
					if out.IsError {
						data, _ := json.Marshal(out)
						t.Fatalf("approved call failed %s", data)
					}
					mu.Lock()
					if writes != 1 || (tc.Risk != "medium" && prompts < 4) || (tc.Risk == "medium" && prompts != 0) {
						t.Errorf("writes=%d prompts=%d", writes, prompts)
					}
					savedPath := lastBackup
					mode = "write-fail"
					mu.Unlock()
					data, _ := json.Marshal(out)
					if !strings.Contains(string(data), savedPath) || !strings.Contains(string(data), "automatic_rollback") {
						t.Errorf("missing backup contract %s", data)
					}
					out = call(args)
					data, _ = json.Marshal(out)
					mu.Lock()
					if writes != 2 {
						t.Error("automatic retry or skipped write")
					}
					mu.Unlock()
					if !out.IsError || !strings.Contains(string(data), "Retained backup") || strings.Contains(string(data), "PRIVATE") {
						t.Errorf("unsafe failure %s", data)
					}
					if _, err := os.Stat(savedPath); err != nil {
						t.Error("lost previous recovery record")
					}
				})
			}
		})
	}
}
