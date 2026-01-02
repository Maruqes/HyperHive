package db

import (
	"context"
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func InitDB(ctx context.Context) {
	var err error
	dsn := "file:data.db?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=15000&_foreign_keys=on"
	DB, err = sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatal(err)
	}
	// Allow multiple concurrent readers; SQLite will serialize writers in WAL mode.
	DB.SetMaxOpenConns(8)
	DB.SetMaxIdleConns(4)
	if err := DB.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
}
