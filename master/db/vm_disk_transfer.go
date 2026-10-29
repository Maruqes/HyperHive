package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Terminal records are retained for audit; uncertain records never expire.
type DiskOperation struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	DiskID           int      `json:"disk_id"`
	Name             string   `json:"name"`
	SourceNFS        int      `json:"source_nfs"`
	DestinationNFS   int      `json:"destination_nfs"`
	SourceShare      NFSShare `json:"source_share"`
	DestinationShare NFSShare `json:"destination_share"`
	SourcePath       string   `json:"source_path"`
	DestinationPath  string   `json:"destination_path"`
	SourceBase       string   `json:"source_base"`
	DestinationBase  string   `json:"destination_base"`
	VMName           string   `json:"vm_name"`
	MachineName      string   `json:"machine_name"`
	Length           int64    `json:"length"`
	SHA256           string   `json:"sha256"`
	Phase            string   `json:"phase"`
	Error            string   `json:"error,omitempty"`
}

func CreateDiskOperationsTable(ctx context.Context) error {
	_, err := DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS vm_disk_operations (
 id TEXT PRIMARY KEY, disk_id INTEGER NOT NULL, name TEXT NOT NULL,
 phase TEXT NOT NULL, record TEXT NOT NULL, updated_at TEXT NOT NULL, vm_name TEXT NOT NULL DEFAULT '');
 CREATE UNIQUE INDEX IF NOT EXISTS vm_disk_operation_active_vm ON vm_disk_operations(vm_name) WHERE vm_name <> '' AND phase NOT IN ('complete','aborted');
 CREATE UNIQUE INDEX IF NOT EXISTS vm_disk_operation_active_name ON vm_disk_operations(name) WHERE phase NOT IN ('complete','aborted');
 CREATE UNIQUE INDEX IF NOT EXISTS vm_disk_operation_active_disk ON vm_disk_operations(disk_id) WHERE disk_id > 0 AND phase NOT IN ('complete','aborted');`)
	return err
}

// FULL commits are required before deleting the only source copy; the normal
// application pool otherwise uses WAL synchronous=NORMAL.
func durableDiskConnection(ctx context.Context) (*sql.Conn, error) {
	conn, err := DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func BeginDiskOperation(ctx context.Context, op *DiskOperation) error {
	conn, connectionErr := durableDiskConnection(ctx)
	if connectionErr != nil {
		return connectionErr
	}
	defer conn.Close()

	op.ID = uuid.NewString()
	op.Phase = "reserved"
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO vm_disk_operations(id,disk_id,name,phase,record,updated_at,vm_name) VALUES(?,?,?,?,?,?,?)`, op.ID, op.DiskID, op.Name, op.Phase, string(data), time.Now().UTC().Format(time.RFC3339), op.VMName)
	if err != nil {
		return fmt.Errorf("disk has a conflicting or unresolved operation: %w", err)
	}
	return nil
}
func SaveDiskOperation(ctx context.Context, op *DiskOperation, previous string) error {
	conn, connectionErr := durableDiskConnection(ctx)
	if connectionErr != nil {
		return connectionErr
	}
	defer conn.Close()

	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	res, err := conn.ExecContext(ctx, `UPDATE vm_disk_operations SET disk_id=?,phase=?,record=?,updated_at=? WHERE id=? AND phase=?`, op.DiskID, op.Phase, string(data), time.Now().UTC().Format(time.RFC3339), op.ID, previous)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("operation state changed")
	}
	return nil
}

func AbortDiskOperation(ctx context.Context, op *DiskOperation) error {
	previous := op.Phase
	op.Phase = "aborted"
	if err := SaveDiskOperation(ctx, op, previous); err != nil {
		op.Phase = previous
		return err
	}
	return nil
}
func GetDiskOperation(ctx context.Context, id string) (*DiskOperation, error) {
	var data string
	err := DB.QueryRowContext(ctx, `SELECT record FROM vm_disk_operations WHERE id=?`, id).Scan(&data)
	if err != nil {
		return nil, err
	}
	var op DiskOperation
	if err = json.Unmarshal([]byte(data), &op); err != nil {
		return nil, err
	}
	return &op, nil
}
func ListDiskOperations(ctx context.Context, limit int) ([]DiskOperation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := DB.QueryContext(ctx, `SELECT record FROM vm_disk_operations WHERE phase NOT IN ('complete','aborted') ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ops := []DiskOperation{}
	for rows.Next() {
		var data string
		var op DiskOperation
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func CommitDiskMove(ctx context.Context, op *DiskOperation, disk *VMDisk, destination *VMDisk) error {
	conn, connectionErr := durableDiskConnection(ctx)
	if connectionErr != nil {
		return connectionErr
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE vm_disks SET nfs_id=?,disk_path=?,folder_path=? WHERE id=? AND nfs_id=? AND disk_path=? AND COALESCE(attached_vm_name,'')=? AND COALESCE(attached_machine_name,'')=?`, destination.NFSID, destination.DiskPath, destination.FolderPath, disk.Id, op.SourceNFS, op.SourcePath, op.VMName, op.MachineName)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("disk metadata changed; both copies preserved")
	}
	previous := op.Phase
	op.Phase = "committed"
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	res, err = tx.ExecContext(ctx, `UPDATE vm_disk_operations SET phase=?,record=? WHERE id=? AND phase=?`, op.Phase, string(data), op.ID, previous)
	if err != nil {
		return err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("operation state changed")
	}
	return tx.Commit()
}
func CommitDiskImport(ctx context.Context, op *DiskOperation, disk *VMDisk) error {
	conn, connectionErr := durableDiskConnection(ctx)
	if connectionErr != nil {
		return connectionErr
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO vm_disks(name,nfs_id,disk_path,folder_path,format,size_gb,created_at) VALUES(?,?,?,?,?,?,?)`, op.Name, op.DestinationNFS, disk.DiskPath, disk.FolderPath, disk.Format, disk.SizeGB, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	op.DiskID = int(id)
	previous := op.Phase
	op.Phase = "complete"
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	res, err = tx.ExecContext(ctx, `UPDATE vm_disk_operations SET disk_id=?,phase=?,record=? WHERE id=? AND phase=?`, op.DiskID, op.Phase, string(data), op.ID, previous)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("operation state changed")
	}
	return tx.Commit()
}

// BeginDiskMutation uses the same durable reservation as transfers. Callers keep
// uncertain RPC outcomes reserved for operator review rather than expiring locks.
func BeginDiskMutation(ctx context.Context, id int, kind string, vmName ...string) (*DiskOperation, error) {
	disk, err := GetVMDiskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if disk == nil {
		return nil, sql.ErrNoRows
	}
	op := &DiskOperation{Kind: kind, DiskID: id, Name: disk.Name, VMName: disk.AttachedVMName}
	if len(vmName) > 0 {
		op.VMName = strings.TrimSpace(vmName[0])
	}
	if err = BeginDiskOperation(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}
