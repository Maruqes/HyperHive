package db

import (
	"database/sql"
	"fmt"
	"time"
)

type VirshBackup struct {
	Id        int
	Name      string
	Path      string
	NfsId     int
	CreatedAt string
	Automatic bool
}

func CreateTableBackups() error {
	query := `
	CREATE TABLE IF NOT EXISTS virsh_backups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		nfsmount_id INT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		automatic BOOLEAN DEFAULT 0
	);
	`
	_, err := DB.Exec(query)
	return err
}

func InsertVirshBackup(b *VirshBackup) error {
	query := `INSERT INTO virsh_backups (name, path, nfsmount_id, automatic) VALUES (?, ?, ?, ?)`
	result, err := DB.Exec(query, b.Name, b.Path, b.NfsId, b.Automatic)
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
	rows, err := DB.Query("SELECT id, name, path, nfsmount_id, created_at, automatic FROM virsh_backups")
	if err != nil {
		return nil, fmt.Errorf("failed to query all backups: %v", err)
	}
	defer rows.Close()

	var backups []VirshBackup
	for rows.Next() {
		var b VirshBackup
		if err := rows.Scan(&b.Id, &b.Name, &b.Path, &b.NfsId, &b.CreatedAt, &b.Automatic); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		backups = append(backups, b)
	}

	return backups, nil
}

func GetVirshBackupById(id int) (*VirshBackup, error) {
	query := `SELECT id, name, path, nfsmount_id, created_at, automatic FROM virsh_backups WHERE id = ?`
	row := DB.QueryRow(query, id)

	var b VirshBackup
	if err := row.Scan(&b.Id, &b.Name, &b.Path, &b.NfsId, &b.CreatedAt, &b.Automatic); err != nil {
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

func GetAutomaticBackups(vmName string) ([]*VirshBackup, error) {
	query := `SELECT id, name, path, nfsmount_id, created_at, automatic 
			  FROM virsh_backups 
			  WHERE name = ? AND automatic = 1 
			  ORDER BY created_at DESC`

	rows, err := DB.Query(query, vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to query automatic backups: %v", err)
	}
	defer rows.Close()

	var backups []*VirshBackup
	for rows.Next() {
		var b VirshBackup
		if err := rows.Scan(&b.Id, &b.Name, &b.Path, &b.NfsId, &b.CreatedAt, &b.Automatic); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		backups = append(backups, &b)
	}

	return backups, nil
}

func CreateTableAutomaticBackup() error {
	query := `
	CREATE TABLE IF NOT EXISTS automatic_backup (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vm_name TEXT NOT NULL,
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

func (c Clock) Validate() error {
	if c.Hours < 0 || c.Hours > 23 {
		return fmt.Errorf("invalid hour: %d", c.Hours)
	}
	if c.Minutes < 0 || c.Minutes > 59 {
		return fmt.Errorf("invalid minute: %d", c.Minutes)
	}
	return nil
}

func (c Clock) GetTodayTime() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), c.Hours, c.Minutes, 0, 0, now.Location())
}

// IsBetween checks if the current clock time is within the min-max time window
// Handles midnight wraparound (e.g., 22:00 to 02:00)
func (c Clock) IsBetween(min, max Clock) bool {
	current := c.Hours*60 + c.Minutes
	minMinutes := min.Hours*60 + min.Minutes
	maxMinutes := max.Hours*60 + max.Minutes

	// Handle midnight wraparound (e.g., 22:00 to 02:00)
	if maxMinutes < minMinutes {
		return current >= minMinutes || current <= maxMinutes
	}
	return current >= minMinutes && current <= maxMinutes
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

func GetEnabledAutomaticBackupsAt(clock Clock) ([]AutomaticBackup, error) {
	const query = `
	SELECT id, vm_name, frequency_days, min_time, max_time, nfsmount_id, 
		   max_backups_retain, enabled, last_backup_time
	FROM automatic_backup
	WHERE enabled = 1
	  AND (
		(min_time <= max_time AND min_time <= ? AND max_time >= ?)
		OR
		(min_time > max_time AND (min_time <= ? OR max_time >= ?))
	  );
	`
	rows, err := DB.Query(query, clock.String(), clock.String(), clock.String(), clock.String())
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
