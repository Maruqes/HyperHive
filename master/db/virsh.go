package db

import (
	"context"
	"database/sql"
)

// just to save which vms can be migrated live
type VmLive struct {
	Name string
}

func CreateVmLiveTable(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS vm_live (
		name TEXT PRIMARY KEY
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

func AddVmLive(ctx context.Context, name string) error {
	query := `
	INSERT OR IGNORE INTO vm_live (name)
	VALUES (?);
	`
	_, err := DB.ExecContext(ctx, query, name)
	return err
}

func RemoveVmLive(ctx context.Context, name string) error {
	query := `
	DELETE FROM vm_live
	WHERE name = ?;
	`
	_, err := DB.ExecContext(ctx, query, name)
	return err
}

func GetAllVmLive(ctx context.Context) ([]VmLive, error) {
	const query = `
	SELECT name
	FROM vm_live;
	`
	rows, err := DB.QueryContext(ctx, query)
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
func GetVmLiveByName(ctx context.Context, name string) (*VmLive, error) {
	const query = `
	SELECT name
	FROM vm_live
	WHERE name = ?;
	`
	row := DB.QueryRowContext(ctx, query, name)
	var vm VmLive
	err := row.Scan(&vm.Name)
	if err != nil {
		return nil, err
	}
	return &vm, nil
}

func DoesVmLiveExist(ctx context.Context, name string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM vm_live
	WHERE name = ?;
	`
	var count int
	err := DB.QueryRowContext(ctx, query, name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type AutoStart struct {
	Id     int
	VmName string
}

func CreateTableAutoStart(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS auto_start (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vm_name TEXT NOT NULL UNIQUE
	);
	`
	_, err := DB.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	return err
}

func AddAutoStart(ctx context.Context, vmName string) error {
	query := `
	INSERT INTO auto_start (vm_name)
	VALUES (?);
	`
	_, err := DB.ExecContext(ctx, query, vmName)
	return err
}

func RemoveAutoStart(ctx context.Context, vmName string) error {
	query := `
	DELETE FROM auto_start
	WHERE vm_name = ?;
	`
	_, err := DB.ExecContext(ctx, query, vmName)
	return err
}

func RemoveAutoStartById(ctx context.Context, id int) error {
	query := `
	DELETE FROM auto_start
	WHERE id = ?;
	`
	_, err := DB.ExecContext(ctx, query, id)
	return err
}

func GetAllAutoStart(ctx context.Context) ([]AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start;
	`
	rows, err := DB.QueryContext(ctx, query)
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

func GetAutoStartByName(ctx context.Context, vmName string) (*AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start
	WHERE vm_name = ?;
	`
	row := DB.QueryRowContext(ctx, query, vmName)
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

func GetAutoStartById(ctx context.Context, id int) (*AutoStart, error) {
	const query = `
	SELECT id, vm_name
	FROM auto_start
	WHERE id = ?;
	`
	row := DB.QueryRowContext(ctx, query, id)
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

func DoesAutoStartExist(ctx context.Context, vmName string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM auto_start
	WHERE vm_name = ?;
	`
	var count int
	err := DB.QueryRowContext(ctx, query, vmName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
