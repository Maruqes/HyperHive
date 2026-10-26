package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrations embed.FS

func migrate(database *sql.DB, files fs.FS) error {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return err
	}
	var current int
	if err := database.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(entries) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, len(entries))
	}
	for index, entry := range entries {
		version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if err != nil || version != index+1 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			return fmt.Errorf("invalid migration sequence at %s", entry.Name())
		}
		if version <= current {
			continue
		}
		script, err := fs.ReadFile(files, "migrations/"+entry.Name())
		if err != nil {
			return err
		}
		transaction, err := database.Begin()
		if err != nil {
			return err
		}
		if _, err = transaction.Exec(string(script)); err == nil {
			_, err = transaction.Exec(fmt.Sprintf("PRAGMA user_version = %d", version))
		}
		if err != nil {
			transaction.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := transaction.Commit(); err != nil {
			return err
		}
	}
	return nil
}
