package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

type VMAttachDiskInput struct {
	VMName   string `json:"vm_name"`
	VMDiskID int64  `json:"vm_disk_id"`
}
type VMBackupInput struct {
	VMName string `json:"vm_name"`
	NFSID  int64  `json:"nfs_id"`
}
type VMCloneSettings struct {
	NewName    string `json:"new_name"`
	TemplateID int64  `json:"template_id,omitempty"`
}
type VMCloneInput struct {
	VMName             string `json:"vm_name"`
	DestinationNFS     int64  `json:"dest_nfs"`
	DestinationMachine string `json:"dest_machine_name"`
	VMCloneSettings
}

func (c *Client) AttachVMDisk(ctx context.Context, in VMAttachDiskInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "POST", "/virsh/vm_disk/"+url.PathEscape(in.VMName), struct {
		ID int64 `json:"vm_disk_id"`
	}{in.VMDiskID})
	if err != nil {
		return nil, err
	}
	if err = ValidateVMResponse("VMAttachedDisk", raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *Client) BackupVM(ctx context.Context, in VMBackupInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/backup/"+url.PathEscape(in.VMName)+"/"+strconv.FormatInt(in.NFSID, 10), nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true,"background":true,"completion_confirmed":false}`), nil
}
func (c *Client) CloneVM(ctx context.Context, in VMCloneInput) (json.RawMessage, error) {
	if err := ValidateVMWrite(in); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "POST", "/virsh/cloneVM/"+url.PathEscape(in.VMName)+"/"+strconv.FormatInt(in.DestinationNFS, 10)+"/"+url.PathEscape(in.DestinationMachine), in.VMCloneSettings, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true,"background":true,"completion_confirmed":false}`), nil
}
func (c *Client) DisableVMAutomaticBackup(ctx context.Context, in VMIDInput) (json.RawMessage, error) {
	if err := ValidateVMID(in.ID); err != nil {
		return nil, err
	}
	if err := c.request(ctx, "PATCH", "/virsh/autopak/disable/"+strconv.FormatInt(in.ID, 10), nil, nil); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"accepted":true}`), nil
}

// SnapshotVMNFS is an internal reviewed inventory getter, never an MCP dispatcher.
func (c *Client) SnapshotVMNFS(ctx context.Context, id int64) (json.RawMessage, error) {
	if err := ValidateVMID(id); err != nil {
		return nil, err
	}
	raw, err := c.vmJSON(ctx, "GET", "/nfs/list", nil)
	if err != nil {
		return nil, err
	}
	var records []struct {
		NFS json.RawMessage `json:"NfsShare"`
	}
	if json.Unmarshal(raw, &records) != nil {
		return nil, errors.New("invalid NFS snapshot")
	}
	for _, record := range records {
		var share struct {
			ID          int64 `json:"Id"`
			MachineName string
			Target      string
			Source      string
			FolderPath  string
		}
		if json.Unmarshal(record.NFS, &share) != nil {
			return nil, errors.New("invalid NFS snapshot")
		}
		if share.ID == id {
			if share.MachineName == "" || share.Target == "" || share.Source == "" || share.FolderPath == "" {
				return nil, errors.New("incomplete NFS snapshot")
			}
			return record.NFS, nil
		}
	}
	return nil, errors.New("destination NFS share not found")
}
