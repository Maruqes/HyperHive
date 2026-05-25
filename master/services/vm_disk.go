package services

import (
	"context"
	"fmt"
	"strings"

	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/protocol"
	"512SvMan/virsh"
	vmdisk "512SvMan/vm_disk"

	nfsGrpc "github.com/Maruqes/512SvMan/api/proto/nfs"
	vmDiskGrpc "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"google.golang.org/grpc"
)

type VMDiskService struct{}

type VMDiskResult struct {
	Id            int     `json:"id"`
	Name          string  `json:"name"`
	NFSID         int     `json:"nfs_id"`
	DiskPath      string  `json:"disk_path"`
	FolderPath    string  `json:"folder_path"`
	Format        string  `json:"format"`
	SizeGB        int64   `json:"size_gb"`
	OccupiedGB    float64 `json:"occupied_gb"`
	OccupiedBytes int64   `json:"occupied_bytes"`
	AttachedVM    string  `json:"attached_vm_name"`
	StatusError   string  `json:"status_error,omitempty"`
}

func (s *VMDiskService) List(ctx context.Context) ([]VMDiskResult, error) {
	disks, err := db.GetAllVMDisk(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]VMDiskResult, 0, len(disks))
	for i := range disks {
		result := convertDBVMDisk(&disks[i])
		info, err := s.vmDiskInfo(ctx, &disks[i])
		if err != nil {
			result.StatusError = err.Error()
			result.OccupiedGB = -1
			result.OccupiedBytes = -1
		} else {
			result.DiskPath = info.GetDiskPath()
			result.FolderPath = info.GetFolderPath()
			result.Format = info.GetFormat()
			result.SizeGB = info.GetSizeGB()
			result.OccupiedGB = info.GetOccupiedGB()
			result.OccupiedBytes = info.GetOccupiedBytes()
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *VMDiskService) Create(ctx context.Context, name string, nfsID int, sizeGB int64, format string) (*VMDiskResult, error) {
	if exists, err := db.DoesVMDiskNameExist(ctx, name); err != nil {
		return nil, fmt.Errorf("failed to check VM disk name: %w", err)
	} else if exists {
		return nil, fmt.Errorf("VM disk with name %s already exists", name)
	}

	conn, target, err := s.readyNFSConnection(ctx, nfsID)
	if err != nil {
		return nil, err
	}

	res, err := vmdisk.CreateVMDisk(conn.Connection, &vmDiskGrpc.CreateVMDiskRequest{
		BasePath: target,
		Name:     name,
		SizeGB:   sizeGB,
		Format:   format,
	})
	if err != nil {
		return nil, err
	}
	if res == nil || !res.GetOk() {
		return nil, fmt.Errorf("failed to create VM disk")
	}
	if err := nfs.Sync(conn.Connection); err != nil {
		return nil, fmt.Errorf("failed to sync NFS after creating VM disk: %w", err)
	}
	id, err := db.AddVMDisk(ctx, name, nfsID, res.GetDiskPath(), res.GetFolderPath(), res.GetFormat(), res.GetSizeGB())
	if err != nil {
		_, _ = vmdisk.DeleteVMDisk(conn.Connection, &vmDiskGrpc.VMDiskByNameRequest{BasePath: target, Name: name})
		return nil, fmt.Errorf("failed to register VM disk: %w", err)
	}
	result := convertVMDiskResponse(res)
	result.Id = id
	result.Name = name
	result.NFSID = nfsID
	return result, nil
}

func (s *VMDiskService) Delete(ctx context.Context, id int) (*VMDiskResult, error) {
	disk, err := db.GetVMDiskByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM disk by ID: %w", err)
	}
	if disk == nil {
		return nil, fmt.Errorf("VM disk with ID %d not found", id)
	}

	conn, target, err := s.readyNFSConnection(ctx, disk.NFSID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(disk.AttachedVMName) != "" {
		attachedConn := conn
		if machine := strings.TrimSpace(disk.AttachedMachineName); machine != "" {
			attachedConn = protocol.GetConnectionByMachineName(machine)
			if attachedConn == nil || attachedConn.Connection == nil {
				return nil, fmt.Errorf("attached VM machine %s is not connected", machine)
			}
		}
		if err := detachAttachedVMDisk(attachedConn.Connection, disk); err != nil {
			return nil, fmt.Errorf("failed to detach VM disk from %s before delete: %w", disk.AttachedVMName, err)
		}
		if err := db.ClearVMDiskAttachment(ctx, disk.Id); err != nil {
			return nil, fmt.Errorf("failed to clear VM disk attachment: %w", err)
		}
	}

	res, err := vmdisk.DeleteVMDisk(conn.Connection, &vmDiskGrpc.VMDiskByNameRequest{
		BasePath: target,
		Name:     disk.Name,
	})
	if err != nil {
		return nil, err
	}
	if res == nil || !res.GetOk() {
		return nil, fmt.Errorf("failed to delete VM disk")
	}
	if err := nfs.Sync(conn.Connection); err != nil {
		return nil, fmt.Errorf("failed to sync NFS after deleting VM disk: %w", err)
	}
	if err := db.RemoveVMDiskByID(ctx, disk.Id); err != nil {
		return nil, fmt.Errorf("failed to remove VM disk from database: %w", err)
	}
	return convertDBAndRPCVMDisk(disk, res), nil
}

func (s *VMDiskService) Grow(ctx context.Context, id int, sizeGB int64) (*VMDiskResult, error) {
	disk, err := db.GetVMDiskByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM disk by ID: %w", err)
	}
	if disk == nil {
		return nil, fmt.Errorf("VM disk with ID %d not found", id)
	}

	conn, target, err := s.readyNFSConnection(ctx, disk.NFSID)
	if err != nil {
		return nil, err
	}

	res, err := vmdisk.GrowVMDisk(conn.Connection, &vmDiskGrpc.GrowVMDiskRequest{
		BasePath: target,
		Name:     disk.Name,
		SizeGB:   sizeGB,
	})
	if err != nil {
		return nil, err
	}
	if res == nil || !res.GetOk() {
		return nil, fmt.Errorf("failed to grow VM disk")
	}
	if err := nfs.Sync(conn.Connection); err != nil {
		return nil, fmt.Errorf("failed to sync NFS after growing VM disk: %w", err)
	}
	if err := db.UpdateVMDiskSize(ctx, disk.Id, res.GetSizeGB()); err != nil {
		return nil, fmt.Errorf("failed to update VM disk size: %w", err)
	}
	disk.SizeGB = res.GetSizeGB()
	return convertDBAndRPCVMDisk(disk, res), nil
}

func (s *VMDiskService) readyNFSConnection(ctx context.Context, nfsID int) (*protocol.ConnectionsStruct, string, error) {
	if nfsID <= 0 {
		return nil, "", fmt.Errorf("nfs_id must be greater than zero")
	}

	nfsShare, err := db.GetNFSShareByID(ctx, nfsID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get NFS share by ID: %w", err)
	}
	if nfsShare == nil {
		return nil, "", fmt.Errorf("NFS share with ID %d not found", nfsID)
	}

	conn := protocol.GetConnectionByMachineName(nfsShare.MachineName)
	if conn == nil || conn.Connection == nil {
		return nil, "", fmt.Errorf("no connection found for machine: %s", nfsShare.MachineName)
	}

	target := strings.TrimSuffix(nfsShare.Target, "/")
	mount := &nfsGrpc.FolderMount{
		MachineName:     nfsShare.MachineName,
		FolderPath:      nfsShare.FolderPath,
		Source:          nfsShare.Source,
		Target:          target,
		HostNormalMount: nfsShare.HostNormalMount,
	}
	status, err := nfs.GetSharedFolderStatus(conn.Connection, mount)
	if err != nil {
		return nil, "", fmt.Errorf("failed to check NFS share status: %w", err)
	}
	if status == nil || !status.GetWorking() {
		return nil, "", fmt.Errorf("NFS share %d is not mounted on %s", nfsID, nfsShare.MachineName)
	}
	if err := nfs.CheckReadWrite(conn.Connection, target); err != nil {
		return nil, "", fmt.Errorf("NFS share %d is not writable on %s: %w", nfsID, nfsShare.MachineName, err)
	}

	return conn, target, nil
}

func (s *VMDiskService) vmDiskInfo(ctx context.Context, disk *db.VMDisk) (*vmDiskGrpc.VMDiskResponse, error) {
	conn, target, err := s.readyNFSConnection(ctx, disk.NFSID)
	if err != nil {
		return nil, err
	}
	return vmdisk.GetVMDiskInfo(conn.Connection, &vmDiskGrpc.VMDiskByNameRequest{
		BasePath: target,
		Name:     disk.Name,
	})
}

func convertVMDiskResponse(res *vmDiskGrpc.VMDiskResponse) *VMDiskResult {
	return &VMDiskResult{
		DiskPath:      res.GetDiskPath(),
		FolderPath:    res.GetFolderPath(),
		Format:        res.GetFormat(),
		SizeGB:        res.GetSizeGB(),
		OccupiedGB:    res.GetOccupiedGB(),
		OccupiedBytes: res.GetOccupiedBytes(),
	}
}

func convertDBAndRPCVMDisk(disk *db.VMDisk, res *vmDiskGrpc.VMDiskResponse) *VMDiskResult {
	result := convertVMDiskResponse(res)
	result.Id = disk.Id
	result.Name = disk.Name
	result.NFSID = disk.NFSID
	result.AttachedVM = disk.AttachedVMName
	return result
}

func convertDBVMDisk(disk *db.VMDisk) VMDiskResult {
	return VMDiskResult{
		Id:         disk.Id,
		Name:       disk.Name,
		NFSID:      disk.NFSID,
		DiskPath:   disk.DiskPath,
		FolderPath: disk.FolderPath,
		Format:     disk.Format,
		SizeGB:     disk.SizeGB,
		AttachedVM: disk.AttachedVMName,
	}
}

func detachAttachedVMDisk(conn *grpc.ClientConn, disk *db.VMDisk) error {
	if strings.TrimSpace(disk.AttachedVMName) == "" {
		return nil
	}
	_, err := virsh.DetachExternalDisk(conn, disk.AttachedVMName, disk.DiskPath, disk.Format)
	return err
}
