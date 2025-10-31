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

func CreateTableAutomaticBackup() error {
	query := `
	CREATE TABLE IF NOT EXISTS automatic_backup (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vm_name TEXT NOT NULL UNIQUE,
		frequency_days INTEGER NOT NULL DEFAULT 7,
		min_time TEXT NOT NULL DEFAULT '00:00',
		max_time TEXT NOT NULL DEFAULT '23:59',
		nfsmount_id INTEGER,
		max_backups_retain INTEGER DEFAULT 5,
		enabled BOOLEAN DEFAULT 1,
		last_backup_time DATETIME
	);
	`
	_, err := DB.Exec(query)
	return err
}

type Clock struct {
	Hours   int `json:"hours"`
	Minutes int `json:"minutes"`
}

// MarshalText makes Clock encode as a "HH:MM" string in JSON (and other text-based encoders).
func (c Clock) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// UnmarshalText accepts a "HH:MM" string when decoding from JSON (or other text-based decoders).
// It also returns an error for invalid formats.
func (c *Clock) UnmarshalText(data []byte) error {
	parsed, err := ParseClock(string(data))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// String converts Clock to "HH:MM" format
func (c Clock) String() string {
	return fmt.Sprintf("%02d:%02d", c.Hours, c.Minutes)
}

// ParseClock converts "HH:MM" string to Clock
func ParseClock(s string) (Clock, error) {
	var c Clock
	_, err := fmt.Sscanf(s, "%d:%d", &c.Hours, &c.Minutes)
	return c, err
}

type AutomaticBackup struct {
	Id               int
	VmName           string
	FrequencyDays    int
	MinTime          Clock
	MaxTime          Clock
	NfsMountId       int
	MaxBackupsRetain int
	Enabled          bool
	LastBackupTime   *string
}

func AddAutomaticBackup(ab *AutomaticBackup) error {
	query := `
	INSERT INTO automatic_backup (vm_name, frequency_days, min_time, max_time, nfsmount_id, max_backups_retain, enabled)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	result, err := DB.Exec(query, ab.VmName, ab.FrequencyDays, ab.MinTime.String(), ab.MaxTime.String(), ab.NfsMountId, ab.MaxBackupsRetain, ab.Enabled)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	ab.Id = int(id)
	return nil
}

func UpdateAutomaticBackup(ab *AutomaticBackup) error {
	query := `
	UPDATE automatic_backup
	SET frequency_days = ?, min_time = ?, max_time = ?, nfsmount_id = ?, 
	    max_backups_retain = ?, enabled = ?
	WHERE id = ?;
	`
	_, err := DB.Exec(query, ab.FrequencyDays, ab.MinTime.String(), ab.MaxTime.String(), ab.NfsMountId, ab.MaxBackupsRetain, ab.Enabled, ab.Id)
	return err
}

func UpdateAutomaticBackupTimes(id int, lastBackup *string) error {
	query := `
	UPDATE automatic_backup
	SET last_backup_time = ?
	WHERE id = ?;
	`
	_, err := DB.Exec(query, lastBackup, id)
	return err
}

func RemoveAutomaticBackup(vmName string) error {
	query := `
	DELETE FROM automatic_backup
	WHERE vm_name = ?;
	`
	_, err := DB.Exec(query, vmName)
	return err
}

func RemoveAutomaticBackupById(id int) error {
	query := `
	DELETE FROM automatic_backup
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	return err
}

func EnableAutomaticBackup(vmName string) error {
	query := `
	UPDATE automatic_backup
	SET enabled = 1
	WHERE vm_name = ?;
	`
	_, err := DB.Exec(query, vmName)
	return err
}

func DisableAutomaticBackup(vmName string) error {
	query := `
	UPDATE automatic_backup
	SET enabled = 0
	WHERE vm_name = ?;
	`
	_, err := DB.Exec(query, vmName)
	return err
}

func EnableAutomaticBackupById(id int) error {
	query := `
	UPDATE automatic_backup
	SET enabled = 1
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	return err
}

func DisableAutomaticBackupById(id int) error {
	query := `
	UPDATE automatic_backup
	SET enabled = 0
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	return err
}

func GetAllAutomaticBackups() ([]AutomaticBackup, error) {
	const query = `
	SELECT id, vm_name, frequency_days, min_time, max_time, nfsmount_id, 
	       max_backups_retain, enabled, last_backup_time
	FROM automatic_backup;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []AutomaticBackup
	for rows.Next() {
		var ab AutomaticBackup
		var minTimeStr, maxTimeStr string
		if err := rows.Scan(&ab.Id, &ab.VmName, &ab.FrequencyDays, &minTimeStr, &maxTimeStr,
			&ab.NfsMountId, &ab.MaxBackupsRetain, &ab.Enabled, &ab.LastBackupTime); err != nil {
			return nil, err
		}
		ab.MinTime, _ = ParseClock(minTimeStr)
		ab.MaxTime, _ = ParseClock(maxTimeStr)
		backups = append(backups, ab)
	}
	return backups, nil
}

func GetAutomaticBackupByName(vmName string) (*AutomaticBackup, error) {
	const query = `
	SELECT id, vm_name, frequency_days, min_time, max_time, nfsmount_id, 
	       max_backups_retain, enabled, last_backup_time
	FROM automatic_backup
	WHERE vm_name = ?;
	`
	row := DB.QueryRow(query, vmName)
	var ab AutomaticBackup
	var minTimeStr, maxTimeStr string
	err := row.Scan(&ab.Id, &ab.VmName, &ab.FrequencyDays, &minTimeStr, &maxTimeStr,
		&ab.NfsMountId, &ab.MaxBackupsRetain, &ab.Enabled, &ab.LastBackupTime)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ab.MinTime, _ = ParseClock(minTimeStr)
	ab.MaxTime, _ = ParseClock(maxTimeStr)
	return &ab, nil
}

func GetAutomaticBackupById(id int) (*AutomaticBackup, error) {
	const query = `
	SELECT id, vm_name, frequency_days, min_time, max_time, nfsmount_id, 
	       max_backups_retain, enabled, last_backup_time
	FROM automatic_backup
	WHERE id = ?;
	`
	row := DB.QueryRow(query, id)
	var ab AutomaticBackup
	var minTimeStr, maxTimeStr string
	err := row.Scan(&ab.Id, &ab.VmName, &ab.FrequencyDays, &minTimeStr, &maxTimeStr,
		&ab.NfsMountId, &ab.MaxBackupsRetain, &ab.Enabled, &ab.LastBackupTime)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ab.MinTime, _ = ParseClock(minTimeStr)
	ab.MaxTime, _ = ParseClock(maxTimeStr)
	return &ab, nil
}

func GetEnabledAutomaticBackups() ([]AutomaticBackup, error) {
	const query = `
	SELECT id, vm_name, frequency_days, min_time, max_time, nfsmount_id, 
	       max_backups_retain, enabled, last_backup_time
	FROM automatic_backup
	WHERE enabled = 1;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []AutomaticBackup
	for rows.Next() {
		var ab AutomaticBackup
		var minTimeStr, maxTimeStr string
		if err := rows.Scan(&ab.Id, &ab.VmName, &ab.FrequencyDays, &minTimeStr, &maxTimeStr,
			&ab.NfsMountId, &ab.MaxBackupsRetain, &ab.Enabled, &ab.LastBackupTime); err != nil {
			return nil, err
		}
		ab.MinTime, _ = ParseClock(minTimeStr)
		ab.MaxTime, _ = ParseClock(maxTimeStr)
		backups = append(backups, ab)
	}
	return backups, nil
}

func DoesAutomaticBackupExist(vmName string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM automatic_backup
	WHERE vm_name = ?;
	`
	var count int
	err := DB.QueryRow(query, vmName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type AutoStart struct {
	Id     int
	VmName string
}

func CreateTableAutoStart() error {
	query := `
	CREATE TABLE IF NOT EXISTS auto_start (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vm_name TEXT NOT NULL UNIQUE
	);
	`
	_, err := DB.Exec(query)
	if err != nil {
		return err
	}

	return err
}

func AddAutoStart(vmName string) error {
	query := `
	INSERT INTO auto_start (vm_name)
	VALUES (?);
	`
	_, err := DB.Exec(query, vmName)
	return err
}

func RemoveAutoStart(vmName string) error {
	query := `
	DELETE FROM auto_start
	WHERE vm_name = ?;
	`
	_, err := DB.Exec(query, vmName)
	return err
}

func RemoveAutoStartById(id int) error {
	query := `
	DELETE FROM auto_start
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	return err
}

func GetAllAutoStart() ([]AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vms []AutoStart
	for rows.Next() {
		var vm AutoStart
		if err := rows.Scan(&vm.Id, &vm.VmName); err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func GetAutoStartByName(vmName string) (*AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start
	WHERE vm_name = ?;
	`
	row := DB.QueryRow(query, vmName)
	var vm AutoStart
	err := row.Scan(&vm.Id, &vm.VmName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &vm, nil
}

func GetAutoStartById(id int) (*AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start
	WHERE id = ?;
	`
	row := DB.QueryRow(query, id)
	var vm AutoStart
	err := row.Scan(&vm.Id, &vm.VmName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &vm, nil
}

func DoesAutoStartExist(vmName string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM auto_start
	WHERE vm_name = ?;
	`
	var count int
	err := DB.QueryRow(query, vmName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
