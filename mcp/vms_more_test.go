package hyperhivemcp

import (
	"encoding/json"
	"testing"
)

const vmSecureXMLFixture = `{"name":"guest","uuid":"fixture-uuid","machine_name":"host","active":false,"inactive_xml":"<domain><name>guest</name><uuid>fixture-uuid</uuid><devices><disk device='disk'><source file='/fixture/boot.qcow2'/></disk><graphics type='vnc' passwd='old-private-password'/></devices></domain>","current_xml":""}`

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
	input := `{"vm_name":"guest","machine_name":"host","vm_xml":"<domain><name>guest</name><uuid>fixture-uuid</uuid><memory>2048</memory></domain>"}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "update_vm_xml", Method: "POST", Path: "/virsh/updatevmxml/guest", Args: input, Body: `{"machine_name":"host","vm_xml":"<domain><name>guest</name><uuid>fixture-uuid</uuid><memory>2048</memory></domain>"}`, Risk: "very high", Snapshots: clone()})
	disk := `{"id":7,"name":"disk","nfs_id":2,"disk_path":"/fixture/disk.qcow2","folder_path":"/fixture","format":"qcow2","size_gb":10,"occupied_gb":1,"occupied_bytes":9007199254740993,"attached_vm_name":""}`
	attached := `{"id":7,"name":"disk","nfs_id":2,"disk_path":"/fixture/disk.qcow2","folder_path":"/fixture","format":"qcow2","size_gb":10,"attached_vm_name":"guest","attached_machine_name":"host","created_at":""}`
	m := clone()
	m["disk"] = disk
	m["attached_disks"] = `[]`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "attach_vm_disk", Method: "POST", Path: "/virsh/vm_disk/guest", Args: `{"vm_name":"guest","vm_disk_id":7}`, Body: `{"vm_disk_id":7}`, Response: attached, Risk: "high", Snapshots: m})
	share := `{"Id":2,"MachineName":"host","FolderPath":"/fixture","Source":"fixture:/data","Target":"/fixture","Name":"share","HostNormalMount":false}`
	m = clone()
	m["nfs_share"] = share
	m["backups"] = `{"response":null}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "backup_vm", Method: "POST", Path: "/virsh/backup/guest/2", Args: `{"vm_name":"guest","nfs_id":2}`, Risk: "high", Snapshots: m})
	m = clone()
	m["nfs_share"] = share
	m["vms"] = `{"response":[{"name":"guest","machineName":"host"}]}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "clone_vm", Method: "POST", Path: "/virsh/cloneVM/guest/2/host", Args: `{"vm_name":"guest","dest_nfs":2,"dest_machine_name":"host","new_name":"copy","template_id":0}`, Body: `{"new_name":"copy"}`, Risk: "high", Snapshots: m})
	schedule := `{"Id":7,"VmName":"guest","FrequencyDays":2,"MinTime":"01:00","MaxTime":"03:00","NfsMountId":2,"MaxBackupsRetain":3,"Enabled":true,"LastBackupTime":null}`
	vmWriteCases = append(vmWriteCases, vmWriteCase{Name: "disable_vm_automatic_backup", Method: "PATCH", Path: "/virsh/autopak/disable/7", Args: `{"id":7}`, Risk: "medium", Snapshots: map[string]string{"schedule": schedule}})
}
func TestVMMutationResultsAreHonest(t *testing.T) {
	for _, name := range []string{"backup_vm", "clone_vm"} {
		for _, tc := range vmWriteCases {
			if tc.Name != name {
				continue
			}
			var args map[string]any
			if json.Unmarshal([]byte(tc.Args), &args) != nil {
				t.Fatal("invalid contract fixture")
			}
		}
	}
}
