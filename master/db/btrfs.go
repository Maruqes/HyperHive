package db

import (
	"context"
	"database/sql"
	"fmt"
)

type Btrfs struct {
	ID          int    `json:"id"`
	RaidUUID    string `json:"raid_uuid"`
	MountPoint  string `json:"mount_point"`
	Compression string `json:"compression"`
	MachineName string `json:"machine_name"`
}

// CreateBtrfsTable cria a tabela `btrfs` se não existir
func CreateBtrfsTable(ctx context.Context) error {
	const createStmt = `
	CREATE TABLE IF NOT EXISTS btrfs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		raid_uuid TEXT NOT NULL,
		mount_point TEXT NOT NULL,
		compression TEXT,
		machine_name TEXT NOT NULL DEFAULT ''
	);
	`
	const indexStmt = `
	CREATE INDEX IF NOT EXISTS idx_btrfs_raid_uuid ON btrfs(raid_uuid);
	`
	const indexMachineNameStmt = `
	CREATE INDEX IF NOT EXISTS idx_btrfs_machine_name ON btrfs(machine_name);
	`
	if _, err := DB.ExecContext(ctx, createStmt); err != nil {
		return fmt.Errorf("create btrfs table: %w", err)
	}
	if _, err := DB.ExecContext(ctx, indexStmt); err != nil {
		return fmt.Errorf("create btrfs index: %w", err)
	}
	if _, err := DB.ExecContext(ctx, indexMachineNameStmt); err != nil {
		return fmt.Errorf("create btrfs machine index: %w", err)
	}
	return nil
}

// InsertBtrfs insere um novo registo btrfs e retorna o id inserido
func InsertBtrfs(ctx context.Context, raidUUID, mountPoint, compression, machineName string) (int64, error) {
	const query = `
	INSERT INTO btrfs (raid_uuid, mount_point, compression, machine_name)
	VALUES (?, ?, ?, ?);
	`
	res, err := DB.ExecContext(ctx, query, raidUUID, mountPoint, compression, machineName)
	if err != nil {
		return 0, fmt.Errorf("insert btrfs: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id btrfs: %w", err)
	}
	return id, nil
}

// GetBtrfsByID retorna um registo pelo ID
func GetBtrfsByID(ctx context.Context, id int) (Btrfs, error) {
	const query = `
	SELECT id, raid_uuid, mount_point, compression, machine_name
	FROM btrfs
	WHERE id = ?;
	`
	var b Btrfs
	row := DB.QueryRowContext(ctx, query, id)
	var compression sql.NullString
	var machineName sql.NullString
	if err := row.Scan(&b.ID, &b.RaidUUID, &b.MountPoint, &compression, &machineName); err != nil {
		if err == sql.ErrNoRows {
			return Btrfs{}, fmt.Errorf("btrfs not found")
		}
		return Btrfs{}, fmt.Errorf("get btrfs by id: %w", err)
	}
	if compression.Valid {
		b.Compression = compression.String
	} else {
		b.Compression = ""
	}
	if machineName.Valid {
		b.MachineName = machineName.String
	}
	return b, nil
}

// UpdateBtrfs atualiza um registo existente (usa o campo ID)
func UpdateBtrfs(ctx context.Context, b Btrfs) error {
	const query = `
	UPDATE btrfs
	SET raid_uuid = ?, mount_point = ?, compression = ?, machine_name = ?
	WHERE id = ?;
	`
	_, err := DB.ExecContext(ctx, query, b.RaidUUID, b.MountPoint, b.Compression, b.MachineName, b.ID)
	if err != nil {
		return fmt.Errorf("update btrfs: %w", err)
	}
	return nil
}

// DeleteBtrfs remove um registo pelo ID
func DeleteBtrfs(ctx context.Context, id int) (int64, error) {
	const query = `
	DELETE FROM btrfs WHERE id = ?;
	`
	res, err := DB.ExecContext(ctx, query, id)
	if err != nil {
		return 0, fmt.Errorf("delete btrfs: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete btrfs rows affected: %w", err)
	}
	return rows, nil
}

// ListBtrfs retorna todos os registos de btrfs (limit opcional: passa <=0 para sem limite)
func ListBtrfs(ctx context.Context, limit int) ([]Btrfs, error) {
	q := `SELECT id, raid_uuid, mount_point, compression, machine_name FROM btrfs ORDER BY id DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		q = q + ` LIMIT ?`
		rows, err = DB.QueryContext(ctx, q, limit)
	} else {
		rows, err = DB.QueryContext(ctx, q)
	}
	if err != nil {
		return nil, fmt.Errorf("list btrfs: %w", err)
	}
	defer rows.Close()

	var result []Btrfs
	for rows.Next() {
		var b Btrfs
		var compression sql.NullString
		var machineName sql.NullString
		if err := rows.Scan(&b.ID, &b.RaidUUID, &b.MountPoint, &compression, &machineName); err != nil {
			return nil, fmt.Errorf("scan btrfs row: %w", err)
		}
		if compression.Valid {
			b.Compression = compression.String
		}
		if machineName.Valid {
			b.MachineName = machineName.String
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error btrfs: %w", err)
	}
	return result, nil
}

// GetBtrfsByUUIDAndMount returns a single record that matches the given raid UUID and mount point.
// It returns (nil, nil) when no record matches.
func GetBtrfsByUUIDAndMount(ctx context.Context, machineName, raidUUID, mountPoint string) (*Btrfs, error) {
	const query = `
	SELECT id, raid_uuid, mount_point, compression, machine_name
	FROM btrfs
	WHERE machine_name = ? AND raid_uuid = ? AND mount_point = ?
	LIMIT 1;
	`
	var b Btrfs
	var compression sql.NullString
	var machine sql.NullString
	err := DB.QueryRowContext(ctx, query, machineName, raidUUID, mountPoint).Scan(&b.ID, &b.RaidUUID, &b.MountPoint, &compression, &machine)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get btrfs by uuid and mount: %w", err)
	}
	if compression.Valid {
		b.Compression = compression.String
	}
	if machine.Valid {
		b.MachineName = machine.String
	}
	return &b, nil
}

// DeleteBtrfsByUUID removes automatic mount entries filtered by UUID and optionally by machine name.
// If machineName is empty, every entry for the given UUID is removed.
func DeleteBtrfsByUUID(ctx context.Context, raidUUID, machineName string) (int64, error) {
	query := `DELETE FROM btrfs WHERE raid_uuid = ?`
	args := []interface{}{raidUUID}
	if machineName != "" {
		query += ` AND machine_name = ?`
		args = append(args, machineName)
	}

	res, err := DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete btrfs by uuid: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected delete btrfs by uuid: %w", err)
	}
	return rows, nil
}

// GetAllBtrfs returns all btrfs records
func GetAllBtrfs(ctx context.Context) ([]Btrfs, error) {
	return ListBtrfs(ctx, 0)
}

// GetBtrfsByMachineName returns all btrfs records for a specific machine name
func GetBtrfsByMachineName(ctx context.Context, machineName string) ([]Btrfs, error) {
	const query = `SELECT id, raid_uuid, mount_point, compression, machine_name FROM btrfs WHERE machine_name = ? ORDER BY id DESC`
	rows, err := DB.QueryContext(ctx, query, machineName)
	if err != nil {
		return nil, fmt.Errorf("get btrfs by machine name: %w", err)
	}
	defer rows.Close()

	var result []Btrfs
	for rows.Next() {
		var b Btrfs
		var compression sql.NullString
		var machine sql.NullString
		if err := rows.Scan(&b.ID, &b.RaidUUID, &b.MountPoint, &compression, &machine); err != nil {
			return nil, fmt.Errorf("scan btrfs row: %w", err)
		}
		if compression.Valid {
			b.Compression = compression.String
		}
		if machine.Valid {
			b.MachineName = machine.String
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error btrfs: %w", err)
	}
	return result, nil
}
