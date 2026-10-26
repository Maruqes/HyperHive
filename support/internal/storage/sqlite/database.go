package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	filename, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: filename}).String() + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err = migrate(database, migrations); err != nil {
		database.Close()
		return nil, err
	}
	if _, err = database.Exec("INSERT INTO tabs(id,name) SELECT 'general','General' WHERE NOT EXISTS (SELECT 1 FROM tabs)"); err != nil {
		database.Close()
		return nil, err
	}
	return &Store{db: database}, nil
}

func (store *Store) Close() error                   { return store.db.Close() }
func (store *Store) Ping(ctx context.Context) error { return store.db.PingContext(ctx) }
