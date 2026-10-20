package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Flags come from master database state, not libvirt's autostart property.
func vmSnapshotFlag(ctx context.Context, api VMsAPI, name, flag string) (map[string]json.RawMessage, error) {
	snapshots, err := vmSnapshotDomain(ctx, api, name, "")
	if err != nil {
		return nil, err
	}
	var vm map[string]json.RawMessage
	if json.Unmarshal(snapshots["vm"], &vm) != nil {
		return nil, errors.New("invalid VM flags")
	}
	var value bool
	if raw := vm[flag]; len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
		return nil, errors.New("actual previous VM flag unavailable; no change made")
	}
	return snapshots, nil
}
func vmScheduleInventory(ctx context.Context, api VMsAPI) (json.RawMessage, error) {
	raw, err := api.ListVMAutomaticBackups(ctx, client.VMEmptyInput{})
	if err != nil {
		return nil, err
	}
	if client.ValidateVMResponse("ListVMAutomaticBackups", raw) != nil {
		return nil, errors.New("invalid schedule inventory")
	}
	return raw, nil
}
func vmFindSchedule(raw json.RawMessage, id int64) (json.RawMessage, error) {
	var records []json.RawMessage
	if json.Unmarshal(raw, &records) != nil {
		return nil, errors.New("invalid schedule inventory")
	}
	for _, record := range records {
		var old struct {
			ID int64 `json:"Id"`
		}
		if json.Unmarshal(record, &old) != nil {
			return nil, errors.New("invalid schedule identity")
		}
		if old.ID == id {
			return record, nil
		}
	}
	return nil, errors.New("schedule not found")
}
func vmSnapshotScheduleSettings(ctx context.Context, api VMsAPI, in client.VMScheduleSettings) (map[string]json.RawMessage, error) {
	snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
	if err != nil {
		return nil, err
	}
	share, err := api.SnapshotVMNFS(ctx, in.NFSMountID)
	if err != nil {
		return nil, err
	}
	snapshots["nfs_share"] = share
	raw, err := vmScheduleInventory(ctx, api)
	if err != nil {
		return nil, err
	}
	snapshots["schedules"] = vmInventoryRecord(raw)
	return snapshots, nil
}
func registerVMIncrement(server *mcp.Server, api VMsAPI, gate *vmsafety.Gate, store vmsafety.Store) {
	addVMWrite(server, api, gate, store, vmTool("create_vm", "POST", "/virsh/createvm", "Create a VM with explicit settings, optional XML template and live/autostart flags. Saves target inventory, destination NFS and selected template/ISO before creation. Backend can leave partially created resources on failure; no automatic rollback or fabricated resource ID.", vmsafety.High), vmsafety.High, api.CreateVM, func(ctx context.Context, in client.VMCreateInput) (string, map[string]json.RawMessage, error) {
		raw, err := api.ListVMs(ctx, client.VMEmptyInput{})
		if err != nil {
			return "", nil, err
		}
		if client.ValidateVMResponse("ListVMs", raw) != nil {
			return "", nil, errors.New("invalid VM inventory")
		}
		var existing []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &existing) != nil {
			return "", nil, errors.New("invalid VM inventory")
		}
		for _, vm := range existing {
			if vm.Name == in.Name {
				return "", nil, errors.New("target VM already exists")
			}
		}
		share, err := api.SnapshotVMNFS(ctx, in.NFSShareID)
		if err != nil {
			return "", nil, err
		}
		var nfs struct{ HostNormalMount *bool }
		if json.Unmarshal(share, &nfs) != nil || in.Live && (nfs.HostNormalMount == nil || *nfs.HostNormalMount) {
			return "", nil, errors.New("live creation needs a confirmed shared NFS mount")
		}
		snapshots := map[string]json.RawMessage{"vms": vmInventoryRecord(raw), "nfs_share": share}
		if in.TemplateID > 0 {
			if err = vmExtraSnapshot(ctx, snapshots, "template", "GetVMXMLTemplate", func(ctx context.Context) (json.RawMessage, error) {
				return api.GetVMXMLTemplate(ctx, client.VMIDInput{ID: in.TemplateID})
			}); err != nil {
				return "", nil, err
			}
		}
		if in.ISOID > 0 {
			isos, err := api.ListISOs(ctx)
			if err != nil {
				return "", nil, err
			}
			for _, iso := range isos {
				num, _ := iso["Id"].(json.Number)
				id, e := num.Int64()
				if e == nil && id == in.ISOID {
					snapshots["iso"], err = json.Marshal(iso)
					if err != nil {
						return "", nil, err
					}
					break
				}
			}
			if snapshots["iso"] == nil {
				return "", nil, errors.New("selected ISO not found")
			}
		}
		return in.Name, snapshots, nil
	})
	addVMWrite(server, api, gate, store, vmTool("set_vm_autostart", "POST", "/virsh/autostart/{vm_name}", "Set the master-managed VM autostart flag. False removes only its autostart database entry. Saves actual previous flag and secure domain XML. No automatic rollback.", vmsafety.Medium), vmsafety.Medium, api.SetVMAutostart, func(ctx context.Context, in client.VMAutostartInput) (string, map[string]json.RawMessage, error) {
		s, e := vmSnapshotFlag(ctx, api, in.VMName, "autoStart")
		return in.VMName, s, e
	})
	addVMWrite(server, api, gate, store, vmTool("set_vm_live", "POST", "/virsh/vmlive/{vm_name}", "Set the master-managed VM live flag; this does not start or migrate the VM. False removes only its live database entry. Saves actual previous flag and secure domain XML. No automatic rollback.", vmsafety.High), vmsafety.High, api.SetVMLive, func(ctx context.Context, in client.VMEnableInput) (string, map[string]json.RawMessage, error) {
		s, e := vmSnapshotFlag(ctx, api, in.VMName, "isLive")
		return in.VMName, s, e
	})
	const retention = "Automatically enables the schedule; future retention can permanently delete older backups. Saves prior schedule inventory, target VM and NFS state. Times use the backend server timezone. No backup job is started by this API call and no automatic rollback is provided."
	addVMWrite(server, api, gate, store, vmTool("create_vm_automatic_backup", "POST", "/virsh/autobak", "Create an automatic VM backup schedule. "+retention, vmsafety.VeryHigh), vmsafety.VeryHigh, api.CreateVMAutomaticBackup, func(ctx context.Context, in client.VMScheduleSettings) (string, map[string]json.RawMessage, error) {
		s, e := vmSnapshotScheduleSettings(ctx, api, in)
		return in.VMName, s, e
	})
	addVMWrite(server, api, gate, store, vmTool("update_vm_automatic_backup", "PUT", "/virsh/autobak/{id}", "Replace all writable automatic backup schedule settings. "+retention, vmsafety.VeryHigh), vmsafety.VeryHigh, api.UpdateVMAutomaticBackup, func(ctx context.Context, in client.VMUpdateScheduleInput) (string, map[string]json.RawMessage, error) {
		s, e := vmSnapshotScheduleSettings(ctx, api, in.VMScheduleSettings)
		if e != nil {
			return "", nil, e
		}
		var inventory struct {
			Response json.RawMessage `json:"response"`
		}
		json.Unmarshal(s["schedules"], &inventory)
		old, e := vmFindSchedule(inventory.Response, in.ID)
		if e != nil {
			return "", nil, e
		}
		s["schedule"] = old
		return fmt.Sprintf("schedule %d", in.ID), s, nil
	})
	addVMWrite(server, api, gate, store, vmTool("enable_vm_automatic_backup", "PATCH", "/virsh/autopak/enable/{id}", "Enable an existing schedule. Future retention can permanently delete older backups. Saves the exact prior schedule, VM and NFS state; already-enabled schedules are rejected. No automatic rollback.", vmsafety.VeryHigh), vmsafety.VeryHigh, api.EnableVMAutomaticBackup, func(ctx context.Context, in client.VMIDInput) (string, map[string]json.RawMessage, error) {
		raw, e := vmScheduleInventory(ctx, api)
		if e != nil {
			return "", nil, e
		}
		old, e := vmFindSchedule(raw, in.ID)
		if e != nil {
			return "", nil, e
		}
		var schedule struct {
			VMName  string `json:"VmName"`
			NFSID   int64  `json:"NfsMountId"`
			Enabled *bool  `json:"Enabled"`
		}
		if json.Unmarshal(old, &schedule) != nil || schedule.Enabled == nil || *schedule.Enabled {
			return "", nil, errors.New("schedule enabled state unavailable or already enabled")
		}
		s, e := vmSnapshotDomain(ctx, api, schedule.VMName, "")
		if e != nil {
			return "", nil, e
		}
		share, e := api.SnapshotVMNFS(ctx, schedule.NFSID)
		if e != nil {
			return "", nil, e
		}
		s["nfs_share"] = share
		s["schedules"] = vmInventoryRecord(raw)
		s["schedule"] = old
		return fmt.Sprintf("schedule %d", in.ID), s, nil
	})
	registerVMSSH(server, api, gate, store)
}

func vmRequireShutoff(s map[string]json.RawMessage) error {
	var vm struct {
		State *int `json:"state"`
	}
	var secure struct {
		Active *bool `json:"active"`
	}
	if json.Unmarshal(s["vm"], &vm) != nil || vm.State == nil || *vm.State != 5 || json.Unmarshal(s["secure_domain"], &secure) != nil || secure.Active == nil || *secure.Active {
		return errors.New("SSH injection requires a shut-off VM; shut down separately before calling")
	}
	return vmValidatePrimaryDiskSnapshot(s)
}
func registerVMSSH(server *mcp.Server, api VMsAPI, gate *vmsafety.Gate, store vmsafety.Store) {
	addVMTransfer(server, gate, store, vmTool("add_vm_ssh_key", "POST", "/virsh/add_ssh_key/{vm_name}", "Inject one public SSH key into eligible guest users and /etc/skel. Guest must already be shut off. Before injection, streams a retained primary-disk copy to private MCP server storage and saves its size/SHA-256 manifest, preserving prior guest files and ownership. This can consume the full disk-file size; backing files remain external dependencies. No automatic rollback. Protects only against operations serialized by this MCP registry; external starts or disk changes can race.", vmsafety.VeryHigh), vmsafety.VeryHigh, func(in client.VMSSHInput) error { return client.ValidateVMWrite(in) }, func(ctx context.Context, in client.VMSSHInput) (string, map[string]json.RawMessage, error) {
		s, e := vmSnapshotDomain(ctx, api, in.VMName, "")
		if e == nil {
			e = vmRequireShutoff(s)
		}
		return in.VMName, s, e
	}, func(ctx context.Context, in client.VMSSHInput, dir string) (json.RawMessage, error) {
		artifact, e := api.ExportVM(ctx, client.VMNameInput{VMName: in.VMName}, dir)
		if e != nil {
			return nil, e
		}
		// Persist the completed artifact's manifest before the guest customization.
		path := filepath.Join(dir, "ssh-disk.json")
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, errors.New("could not create SSH recovery manifest; no injection made")
		}
		_, writeErr := f.Write(artifact)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return nil, errors.New("could not persist SSH recovery manifest; no injection made")
		}
		d, e := os.Open(dir)
		if e != nil {
			return nil, errors.New("could not open SSH recovery directory; no injection made")
		}
		syncErr := d.Sync()
		closeErr = d.Close()
		if syncErr != nil || closeErr != nil {
			return nil, errors.New("could not persist SSH recovery directory; no injection made")
		}
		current, e := vmSnapshotDomain(ctx, api, in.VMName, "")
		if e != nil {
			return nil, e
		}
		if e = vmRequireShutoff(current); e != nil {
			return nil, e
		}
		// Compare the actual domain identity and disk selection captured before export.
		old, e := os.ReadFile(filepath.Join(dir, "before.json"))
		if e != nil {
			return nil, errors.New("SSH recovery record unavailable")
		}
		var record vmsafety.Record
		if json.Unmarshal(old, &record) != nil {
			return nil, errors.New("invalid SSH recovery record")
		}
		if !vmSameSnapshot(record.Snapshots["secure_domain"], current["secure_domain"]) || !vmSameSnapshot(record.Snapshots["vm"], current["vm"]) {
			return nil, errors.New("VM changed during disk export; no injection made")
		}
		result, e := api.AddVMSSHKey(ctx, in)
		if e != nil {
			return nil, e
		}
		return json.Marshal(struct {
			Result   json.RawMessage `json:"operation"`
			Disk     json.RawMessage `json:"disk_backup"`
			Manifest string          `json:"manifest_path"`
		}{result, artifact, path})
	})
}

// Recovery JSON is pretty printed and HTML escaped on disk. Compare decoded
// values, retaining exact integers, rather than treating formatting as a change.
func vmSameSnapshot(a, b json.RawMessage) bool {
	var av, bv any
	ad := json.NewDecoder(bytes.NewReader(a))
	ad.UseNumber()
	bd := json.NewDecoder(bytes.NewReader(b))
	bd.UseNumber()
	return ad.Decode(&av) == nil && bd.Decode(&bv) == nil && reflect.DeepEqual(av, bv)
}
