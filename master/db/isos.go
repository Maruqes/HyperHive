package db

// create table for isos, file where they are stores and machine name that downloaded them
type ISO struct {
	Id          int
	MachineName string
	FilePath    string
	Name        string
}

func CreateISOTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS isos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		machine_name TEXT NOT NULL,
		name TEXT NOT NULL,
		file_path TEXT NOT NULL
	);
	`
	_, err := DB.Exec(query)
	return err
}

func AddISO(machineName, filePath, name string) error {
	query := `
	INSERT INTO isos (machine_name, file_path, name)
	VALUES (?, ?, ?);
	`
	_, err := DB.Exec(query, machineName, filePath, name)
	return err
}

func GetAllISOs() ([]ISO, error) {
	const query = `
	SELECT id, machine_name, file_path, name
	FROM isos;
	`
	rows, err := DB.Query(query)
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

func RemoveISOByID(id int) error {
	query := `
	DELETE FROM isos
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	return err
}
