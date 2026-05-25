package db

import (
	"context"
	"database/sql"
	"time"
)

type VMDisk struct {
	Id                  int    `json:"id"`
	Name                string `json:"name"`
	NFSID               int    `json:"nfs_id"`
	DiskPath            string `json:"disk_path"`
	FolderPath          string `json:"folder_path"`
	Format              string `json:"format"`
	SizeGB              int64  `json:"size_gb"`
	AttachedVMName      string `json:"attached_vm_name"`
	AttachedMachineName string `json:"attached_machine_name"`
	CreatedAt           string `json:"created_at"`
}

func CreateVMDiskTable(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS vm_disks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		nfs_id INTEGER NOT NULL,
		disk_path TEXT NOT NULL UNIQUE,
		folder_path TEXT NOT NULL,
		format TEXT NOT NULL,
		size_gb INTEGER NOT NULL,
		attached_vm_name TEXT DEFAULT '',
		attached_machine_name TEXT DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

func AddVMDisk(ctx context.Context, name string, nfsID int, diskPath, folderPath, format string, sizeGB int64) (int, error) {
	query := `
	INSERT INTO vm_disks (name, nfs_id, disk_path, folder_path, format, size_gb, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	res, err := DB.ExecContext(ctx, query, name, nfsID, diskPath, folderPath, format, sizeGB, time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

func GetAllVMDisk(ctx context.Context) ([]VMDisk, error) {
	const query = `
	SELECT id, name, nfs_id, disk_path, folder_path, format, size_gb, attached_vm_name, attached_machine_name, created_at
	FROM vm_disks
	ORDER BY id DESC;
	`
	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var disks []VMDisk
	for rows.Next() {
		disk, err := scanVMDisk(rows)
		if err != nil {
			return nil, err
		}
		disks = append(disks, disk)
	}
	return disks, rows.Err()
}

func GetVMDiskByID(ctx context.Context, id int) (*VMDisk, error) {
	const query = `
	SELECT id, name, nfs_id, disk_path, folder_path, format, size_gb, attached_vm_name, attached_machine_name, created_at
	FROM vm_disks
	WHERE id = ?;
	`
	row := DB.QueryRowContext(ctx, query, id)
	disk, err := scanVMDisk(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &disk, nil
}

func GetVMDiskByAttachedVM(ctx context.Context, vmName string) ([]VMDisk, error) {
	const query = `
	SELECT id, name, nfs_id, disk_path, folder_path, format, size_gb, attached_vm_name, attached_machine_name, created_at
	FROM vm_disks
	WHERE attached_vm_name = ?
	ORDER BY id DESC;
	`
	rows, err := DB.QueryContext(ctx, query, vmName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var disks []VMDisk
	for rows.Next() {
		disk, err := scanVMDisk(rows)
		if err != nil {
			return nil, err
		}
		disks = append(disks, disk)
	}
	return disks, rows.Err()
}

func DoesVMDiskNameExist(ctx context.Context, name string) (bool, error) {
	const query = `SELECT COUNT(*) FROM vm_disks WHERE name = ?;`
	var count int
	if err := DB.QueryRowContext(ctx, query, name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func UpdateVMDiskSize(ctx context.Context, id int, sizeGB int64) error {
	_, err := DB.ExecContext(ctx, `UPDATE vm_disks SET size_gb = ? WHERE id = ?;`, sizeGB, id)
	return err
}

func ReserveVMDiskAttachment(ctx context.Context, id int, vmName, machineName string) (bool, error) {
	query := `
	UPDATE vm_disks
	SET attached_vm_name = ?, attached_machine_name = ?
	WHERE id = ? AND COALESCE(attached_vm_name, '') = '';
	`
	res, err := DB.ExecContext(ctx, query, vmName, machineName, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func ClearVMDiskAttachment(ctx context.Context, id int) error {
	_, err := DB.ExecContext(ctx, `UPDATE vm_disks SET attached_vm_name = '', attached_machine_name = '' WHERE id = ?;`, id)
	return err
}

func ClearVMDiskAttachmentsByVM(ctx context.Context, vmName string) error {
	_, err := DB.ExecContext(ctx, `UPDATE vm_disks SET attached_vm_name = '', attached_machine_name = '' WHERE attached_vm_name = ?;`, vmName)
	return err
}

func RemoveVMDiskByID(ctx context.Context, id int) error {
	_, err := DB.ExecContext(ctx, `DELETE FROM vm_disks WHERE id = ?;`, id)
	return err
}

type vmDiskScanner interface {
	Scan(dest ...any) error
}

func scanVMDisk(scanner vmDiskScanner) (VMDisk, error) {
	var disk VMDisk
	var attachedVM sql.NullString
	var attachedMachine sql.NullString
	err := scanner.Scan(
		&disk.Id,
		&disk.Name,
		&disk.NFSID,
		&disk.DiskPath,
		&disk.FolderPath,
		&disk.Format,
		&disk.SizeGB,
		&attachedVM,
		&attachedMachine,
		&disk.CreatedAt,
	)
	if err != nil {
		return VMDisk{}, err
	}
	disk.AttachedVMName = attachedVM.String
	disk.AttachedMachineName = attachedMachine.String
	return disk, nil
}
