package db

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
