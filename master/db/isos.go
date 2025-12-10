package db

import (
	"context"
	"database/sql"
	"errors"
)

// create table for isos, file where they are stores and machine name that downloaded them
type ISO struct {
	Id          int
	MachineName string
	FilePath    string
	Name        string
}

func CreateISOTable(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS isos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		name TEXT NOT NULL,
		file_path TEXT NOT NULL
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

func AddISO(ctx context.Context, machineName, filePath, name string) error {
	query := `
	INSERT INTO isos (machine_name, file_path, name)
	VALUES (?, ?, ?);
	`
	_, err := DB.ExecContext(ctx, query, machineName, filePath, name)
	return err
}

func GetAllISOs(ctx context.Context) ([]ISO, error) {
	const query = `
	SELECT id, machine_name, file_path, name
	FROM isos;
	`
	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var isos []ISO
	for rows.Next() {
		var iso ISO
		if err := rows.Scan(&iso.Id, &iso.MachineName, &iso.FilePath, &iso.Name); err != nil {
			return nil, err
		}
		isos = append(isos, iso)
	}
	return isos, nil
}
func GetIsoByID(ctx context.Context, id int) (*ISO, error) {
	const query = `
	SELECT id, machine_name, file_path, name
	FROM isos
	WHERE id = ?;
	`

	var iso ISO
	err := DB.QueryRowContext(ctx, query, id).Scan(&iso.Id, &iso.MachineName, &iso.FilePath, &iso.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &iso, nil
}

func GetIsoByName(ctx context.Context, name string) (*ISO, error) {
	const query = `
	SELECT id, machine_name, file_path, name
	FROM isos
	WHERE name = ?;
	`

	var iso ISO
	err := DB.QueryRowContext(ctx, query, name).Scan(&iso.Id, &iso.MachineName, &iso.FilePath, &iso.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &iso, nil
}

func RemoveISOByID(ctx context.Context, id int) error {
	query := `
	DELETE FROM isos
	WHERE id = ?;
	`
	_, err := DB.ExecContext(ctx, query, id)
	return err
}
