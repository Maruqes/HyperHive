package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/Maruqes/HyperHive/mcp/internal/vmsafety"
)

func vmInventoryRecord(raw json.RawMessage) json.RawMessage {
	wrapped, _ := json.Marshal(struct {
		Response json.RawMessage `json:"response"`
	}{raw})
	return wrapped
}
func defineAttachVMDisk(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("attach_vm_disk", "POST", "/virsh/vm_disk/{vm_name}", "Attach an existing registered disk to a VM; backend validates ownership/readability. Saves actual old disk metadata, VM settings, secure inactive/current XML and attachment inventory. No automatic rollback."), api.AttachVMDisk, func(ctx context.Context, in client.VMAttachDiskInput) (string, map[string]json.RawMessage, error) {
		_, disk, err := vmSnapshotDisk(ctx, api, in.VMDiskID)
		if err != nil {
			return "", nil, err
		}
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		snapshots["disk"] = disk["disk"]
		if err = vmExtraSnapshot(ctx, snapshots, "attached_disks", "ListVMAttachedDisks", func(ctx context.Context) (json.RawMessage, error) {
			return api.ListVMAttachedDisks(ctx, client.VMNameInput{VMName: in.VMName})
		}); err != nil {
			return "", nil, err
		}
		return in.VMName, snapshots, nil
	})
}

func defineBackupVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("backup_vm", "POST", "/virsh/backup/{vm_name}/{nfs_id}", "Request a manual VM disk backup to NFS. A running guest needs qemu-guest-agent; backend freezes and thaws its filesystems. Returns acceptance only: background completion can fail later and no job ID is supplied. Saves source state, destination share and prior backup inventory. No automatic rollback."), api.BackupVM, func(ctx context.Context, in client.VMBackupInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		share, err := api.SnapshotVMNFS(ctx, in.NFSID)
		if err != nil {
			return "", nil, err
		}
		snapshots["nfs_share"] = share
		raw, err := api.ListVMBackups(ctx, client.VMEmptyInput{})
		if err != nil {
			return "", nil, err
		}
		if client.ValidateVMResponse("ListVMBackups", raw) != nil {
			return "", nil, errors.New("invalid backup inventory")
		}
		snapshots["backups"] = vmInventoryRecord(raw)
		return in.VMName, snapshots, nil
	})
}

func defineCloneVM(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("clone_vm", "POST", "/virsh/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}", "Request a full VM disk clone to a new name/host/NFS; may freeze and thaw a running source guest. Returns acceptance only: detached background completion can fail later; no job ID supplied. Saves actual source state, destination share, target VM inventory and optional selected template. No automatic rollback."), api.CloneVM, func(ctx context.Context, in client.VMCloneInput) (string, map[string]json.RawMessage, error) {
		snapshots, err := vmSnapshotDomain(ctx, api, in.VMName, "")
		if err != nil {
			return "", nil, err
		}
		share, err := api.SnapshotVMNFS(ctx, in.DestinationNFS)
		if err != nil {
			return "", nil, err
		}
		snapshots["nfs_share"] = share
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
		json.Unmarshal(raw, &existing)
		for _, vm := range existing {
			if vm.Name == in.NewName {
				return "", nil, errors.New("target VM already exists")
			}
		}
		snapshots["vms"] = vmInventoryRecord(raw)
		if in.TemplateID > 0 {
			if err = vmExtraSnapshot(ctx, snapshots, "template", "GetVMXMLTemplate", func(ctx context.Context) (json.RawMessage, error) {
				return api.GetVMXMLTemplate(ctx, client.VMIDInput{ID: in.TemplateID})
			}); err != nil {
				return "", nil, err
			}
		}
		return in.VMName + " -> " + in.NewName, snapshots, nil
	})
}

func defineDisableVMAutomaticBackup(api VMsAPI, gate *safety.Gate, store vmsafety.Store) ToolDefinition {
	return addVMWrite(api, gate, store, vmTool("disable_vm_automatic_backup", "PATCH", "/virsh/autopak/disable/{id}", "Disable an existing automatic backup schedule by setting Enabled=false. Saves the exact actual prior schedule. Does not create backups or enable retention deletion. A running background backup may still finish. No automatic rollback."), api.DisableVMAutomaticBackup, func(ctx context.Context, in client.VMIDInput) (string, map[string]json.RawMessage, error) {
		raw, err := api.ListVMAutomaticBackups(ctx, client.VMEmptyInput{})
		if err != nil {
			return "", nil, err
		}
		if client.ValidateVMResponse("ListVMAutomaticBackups", raw) != nil {
			return "", nil, errors.New("invalid schedule inventory")
		}
		var records []json.RawMessage
		json.Unmarshal(raw, &records)
		for _, record := range records {
			var id struct {
				ID int64 `json:"Id"`
			}
			if json.Unmarshal(record, &id) != nil {
				return "", nil, errors.New("invalid schedule snapshot")
			}
			if id.ID == in.ID {
				return fmt.Sprintf("schedule %d", in.ID), map[string]json.RawMessage{"schedule": record}, nil
			}
		}
		return "", nil, errors.New("schedule not found")
	})
}
