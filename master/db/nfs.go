package db

import "database/sql"

//this file will inclide all NFS related database functions

type NFSShare struct {
	Id              int
	MachineName     string
	FolderPath      string // local folder path
	Source          string // nfs server path example-> ip:/mnt/nfs_share
	Target          string // mount path on the VM example-> /mnt/nfs_share
	Name            string // optional name for the share
	HostNormalMount bool   // whether to mount as normal on host
}

func CreateNFSTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS nfs_shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		folder_path TEXT NOT NULL,
		source TEXT NOT NULL,
		target TEXT NOT NULL,
		name TEXT,
		host_normal_mount INTEGER DEFAULT 0,
		UNIQUE(machine_name, folder_path)
	);
	`
	_, err := DB.Exec(query)
	return err
}

func AddNFSShare(machineName, folderPath, source, target, name string, hostNormalMount bool) error {
	query := `
	INSERT INTO nfs_shares (machine_name, folder_path, source, target, name, host_normal_mount)
	VALUES (?, ?, ?, ?, ?, ?);
	`
	_, err := DB.Exec(query, machineName, folderPath, source, target, name, hostNormalMount)
	return err
}

func RemoveNFSShare(machineName, folderPath string) error {
	query := `
	DELETE FROM nfs_shares
	WHERE machine_name = ? AND folder_path = ?;
	`
	_, err := DB.Exec(query, machineName, folderPath)
	return err
}

func GetAllNFShares() ([]NFSShare, error) {
	const query = `
	SELECT id, machine_name, folder_path, source, target, name, host_normal_mount
	FROM nfs_shares;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []NFSShare
	for rows.Next() {
		var share NFSShare
		if err := rows.Scan(&share.Id, &share.MachineName, &share.FolderPath, &share.Source, &share.Target, &share.Name, &share.HostNormalMount); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shares, nil
}

func DoesExistNFSShare(machineName, folderPath string) (bool, error) {
	const query = `
	SELECT COUNT(*)
	FROM nfs_shares
	WHERE machine_name = ? AND folder_path = ?;
	`
	var count int
	err := DB.QueryRow(query, machineName, folderPath).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// retorna todas as maquinas que tem pastas compartilhadas
func GetAllMachineNamesWithShares() ([]string, error) {
	const query = `
	SELECT DISTINCT machine_name
	FROM nfs_shares;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machineNames []string
	for rows.Next() {
		var machineName string
		if err := rows.Scan(&machineName); err != nil {
			return nil, err
		}
		machineNames = append(machineNames, machineName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return machineNames, nil
}
func GetNFSharesByMachineName(machineName string) ([]NFSShare, error) {
	const query = `
	SELECT machine_name, folder_path, source, target, name, host_normal_mount
	FROM nfs_shares
	WHERE machine_name = ?;
	`
	rows, err := DB.Query(query, machineName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []NFSShare
	for rows.Next() {
		var share NFSShare
		if err := rows.Scan(&share.MachineName, &share.FolderPath, &share.Source, &share.Target, &share.Name, &share.HostNormalMount); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shares, nil
}

func GetNFSShareByMachineAndFolder(machineName, folderPath string) (*NFSShare, error) {
	const query = `
	SELECT id, machine_name, folder_path, source, target, name, host_normal_mount
	FROM nfs_shares
	WHERE machine_name = ? AND folder_path = ?;
	`

	var share NFSShare
	err := DB.QueryRow(query, machineName, folderPath).Scan(
		&share.Id,
		&share.MachineName,
		&share.FolderPath,
		&share.Source,
		&share.Target,
		&share.Name,
		&share.HostNormalMount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}

func GetNFSShareByID(id int) (*NFSShare, error) {
	const query = `
	SELECT id, machine_name, folder_path, source, target, name, host_normal_mount
	FROM nfs_shares
	WHERE id = ?;
	`
	var share NFSShare
	err := DB.QueryRow(query, id).Scan(&share.Id, &share.MachineName, &share.FolderPath, &share.Source, &share.Target, &share.Name, &share.HostNormalMount)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &share, nil
}
