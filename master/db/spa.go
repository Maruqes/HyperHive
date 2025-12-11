package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SPAPort represents a SPA-protected port stored in the database.
type SPAPort struct {
	ID           int
	Port         int
	PasswordHash string
	CreatedAt    time.Time
}

// CreateSPAPortsTable ensures the spa_ports table exists.
func CreateSPAPortsTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS spa_ports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		port INTEGER NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

// UpsertSPAPort inserts or updates the password hash for a port.
func UpsertSPAPort(ctx context.Context, port int, passwordHash string) error {
	const query = `
	INSERT INTO spa_ports (port, password_hash)
	VALUES (?, ?)
	ON CONFLICT(port) DO UPDATE SET password_hash = excluded.password_hash;
	`
	if _, err := DB.ExecContext(ctx, query, port, passwordHash); err != nil {
		return fmt.Errorf("upsert spa port: %w", err)
	}
	return nil
}

// DeleteSPAPort removes a SPA port entry.
func DeleteSPAPort(ctx context.Context, port int) error {
	const query = `
	DELETE FROM spa_ports
	WHERE port = ?;
	`
	res, err := DB.ExecContext(ctx, query, port)
	if err != nil {
		return fmt.Errorf("delete spa port: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSPAPort returns the SPA entry for a given port.
func GetSPAPort(ctx context.Context, port int) (*SPAPort, error) {
	const query = `
	SELECT id, port, password_hash, created_at
	FROM spa_ports
	WHERE port = ?;
	`
	var entry SPAPort
	err := DB.QueryRowContext(ctx, query, port).Scan(&entry.ID, &entry.Port, &entry.PasswordHash, &entry.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get spa port: %w", err)
	}
	return &entry, nil
}

// ListSPAPorts returns all configured SPA ports.
func ListSPAPorts(ctx context.Context) ([]SPAPort, error) {
	const query = `
	SELECT id, port, password_hash, created_at
	FROM spa_ports
	ORDER BY created_at ASC;
	`
	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list spa ports: %w", err)
	}
	defer rows.Close()

	var out []SPAPort
	for rows.Next() {
		var entry SPAPort
		if err := rows.Scan(&entry.ID, &entry.Port, &entry.PasswordHash, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan spa port: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate spa ports: %w", err)
	}
	return out, nil
}
