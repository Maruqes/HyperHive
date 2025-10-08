package db

//this file will inclide all NFS related database functions

type NFSShare struct {
	Id          int
	MachineName string
	FolderPath  string
	Source      string
	Target      string
}

func CreateNFSTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS nfs_shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		folder_path TEXT NOT NULL,
		source TEXT NOT NULL,
		target TEXT NOT NULL
	);
	`
	_, err := DB.Exec(query)
	return err
}

func AddNFSShare(machineName, folderPath, source, target string) error {
	query := `
	INSERT INTO nfs_shares (machine_name, folder_path, source, target)
	VALUES (?, ?, ?, ?);
	`
	_, err := DB.Exec(query, machineName, folderPath, source, target)
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
	SELECT machine_name, folder_path, source, target
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
		if err := rows.Scan(&share.MachineName, &share.FolderPath, &share.Source, &share.Target); err != nil {
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

//retorna todas as maquinas que tem pastas compartilhadas
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
	SELECT machine_name, folder_path, source, target
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
		if err := rows.Scan(&share.MachineName, &share.FolderPath, &share.Source, &share.Target); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shares, nil
}