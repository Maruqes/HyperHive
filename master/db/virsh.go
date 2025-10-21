package db

import (
	"database/sql"
	"fmt"
)

// just to save which vms can be migrated live
type VmLive struct {
	Name string
}

func CreateVmLiveTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS vm_live (
		name TEXT PRIMARY KEY
	);
	`
	_, err := DB.Exec(query)
	return err
}

func AddVmLive(name string) error {
	query := `
	INSERT INTO vm_live (name)
	VALUES (?);
	`
	_, err := DB.Exec(query, name)
	return err
}

func RemoveVmLive(name string) error {
	query := `
	DELETE FROM vm_live
	WHERE name = ?;
	`
	_, err := DB.Exec(query, name)
	return err
}

func GetAllVmLive() ([]VmLive, error) {
	const query = `
	SELECT name
	FROM vm_live;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vms []VmLive
	for rows.Next() {
		var vm VmLive
		if err := rows.Scan(&vm.Name); err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, nil
}
func GetVmLiveByName(name string) (*VmLive, error) {
	const query = `
	SELECT name
	FROM vm_live
	WHERE name = ?;
	`
	row := DB.QueryRow(query, name)
	var vm VmLive
	err := row.Scan(&vm.Name)
	if err != nil {
		return nil, err
	}
	return &vm, nil
}

func DoesVmLiveExist(name string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM vm_live
	WHERE name = ?;
	`
	var count int
	err := DB.QueryRow(query, name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func CreateTableBackups() error {
	query := `
	CREATE TABLE IF NOT EXISTS virsh_backups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		nfsmount_id INT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := DB.Exec(query)
	return err
}

type VirshBackup struct {
	Id        int
	Name      string
	Path      string
	NfsId     int
	CreatedAt string
}

func InsertVirshBackup(b *VirshBackup) error {
	query := `INSERT INTO virsh_backups (name, path, nfsmount_id) VALUES (?, ?, ?)`
	result, err := DB.Exec(query, b.Name, b.Path, b.NfsId)
	if err != nil {
		return fmt.Errorf("failed to insert virsh backup: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %v", err)
	}
	b.Id = int(id)
	return nil
}

func GetAllVirshBackups() ([]VirshBackup, error) {
	rows, err := DB.Query("SELECT id, name, path, nfsmount_id, created_at FROM virsh_backups")
	if err != nil {
		return nil, fmt.Errorf("failed to query all backups: %v", err)
	}
	defer rows.Close()

	var backups []VirshBackup
	for rows.Next() {
		var b VirshBackup
		if err := rows.Scan(&b.Id, &b.Name, &b.Path, &b.NfsId, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		backups = append(backups, b)
	}

	return backups, nil
}

func GetVirshBackupById(id int) (*VirshBackup, error) {
	query := `SELECT id, name, path, nfsmount_id, created_at FROM virsh_backups WHERE id = ?`
	row := DB.QueryRow(query, id)

	var b VirshBackup
	if err := row.Scan(&b.Id, &b.Name, &b.Path, &b.NfsId, &b.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query backup by ID: %v", err)
	}

	return &b, nil
}

func DeleteVirshBackupById(id int) error {
	query := `DELETE FROM virsh_backups WHERE id = ?`
	_, err := DB.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete virsh backup: %v", err)
	}
	return nil
}
