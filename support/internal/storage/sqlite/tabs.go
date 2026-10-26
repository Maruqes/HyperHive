package sqlite

import (
	"database/sql"
	"fmt"

	"hyperhive/support/internal/domain"
)

func (store *Store) Tabs() ([]domain.Tab, error) {
	rows, err := store.db.Query("SELECT id,name FROM tabs ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tabs := []domain.Tab{}
	for rows.Next() {
		var tab domain.Tab
		if err := rows.Scan(&tab.ID, &tab.Name); err != nil {
			return nil, err
		}
		tabs = append(tabs, tab)
	}
	return tabs, rows.Err()
}
func (store *Store) SaveTab(tab domain.Tab, create bool) error {
	if create {
		_, err := store.db.Exec("INSERT INTO tabs(id,name) VALUES(?,?)", tab.ID, tab.Name)
		return err
	}
	return affected(store.db.Exec("UPDATE tabs SET name=? WHERE id=?", tab.Name, tab.ID))
}
func (store *Store) DeleteTab(id string) error {
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM apps WHERE tab_id=?", id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return domain.Conflict("tab still contains Apps")
	}
	return affected(store.db.Exec("DELETE FROM tabs WHERE id=?", id))
}
func (store *Store) tabExists(id string) error {
	var exists int
	if err := store.db.QueryRow("SELECT 1 FROM tabs WHERE id=?", id).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return domain.Invalid("tab_id does not exist")
		}
		return fmt.Errorf("read tab: %w", err)
	}
	return nil
}
