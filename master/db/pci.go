package db

import (
	"context"
	"time"
)

// GPUMode is the mode a GPU should be in. It is the intended state; the host driver
// binding and the VM definitions are the actual state.
type GPUMode struct {
	MachineName string
	GPUAddress  string
	Mode        string // none, host or vm
	VMName      string // only for mode vm
	LastError   string // why the last reconcile could not apply the mode, empty when applied
}

func CreateGPUModesTable(ctx context.Context) error {
	_, err := DB.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS gpu_modes (
		machine_name TEXT NOT NULL,
		gpu_address TEXT NOT NULL,
		mode TEXT NOT NULL,
		vm_name TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (machine_name, gpu_address)
	) WITHOUT ROWID`)
	return err
}

// GetGPUModes returns the stored modes of one machine by GPU address.
func GetGPUModes(ctx context.Context, machineName string) (map[string]GPUMode, error) {
	rows, err := DB.QueryContext(ctx,
		"SELECT gpu_address, mode, vm_name, last_error FROM gpu_modes WHERE machine_name = ?", machineName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modes := map[string]GPUMode{}
	for rows.Next() {
		mode := GPUMode{MachineName: machineName}
		if err := rows.Scan(&mode.GPUAddress, &mode.Mode, &mode.VMName, &mode.LastError); err != nil {
			return nil, err
		}
		modes[mode.GPUAddress] = mode
	}
	return modes, rows.Err()
}

func SaveGPUMode(ctx context.Context, mode GPUMode) error {
	_, err := DB.ExecContext(ctx, `
	INSERT INTO gpu_modes (machine_name, gpu_address, mode, vm_name, last_error, updated_at) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(machine_name, gpu_address) DO UPDATE SET
		mode = excluded.mode, vm_name = excluded.vm_name, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		mode.MachineName, mode.GPUAddress, mode.Mode, mode.VMName, mode.LastError, time.Now().Unix())
	return err
}
