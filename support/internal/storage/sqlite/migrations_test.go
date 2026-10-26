package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMigrationsPreserveExistingDataAndRollback(t *testing.T) {
	database, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	files := fstest.MapFS{
		"migrations/001_initial.sql": {Data: []byte("CREATE TABLE IF NOT EXISTS evidence (value TEXT); INSERT INTO evidence VALUES ('keep');")},
	}
	if err := migrate(database, files); err != nil {
		t.Fatal(err)
	}
	files["migrations/002_extend.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE evidence ADD COLUMN label TEXT;")}
	if err := migrate(database, files); err != nil {
		t.Fatal(err)
	}
	if err := migrate(database, files); err != nil {
		t.Fatal(err)
	}
	var count, version int
	if err := database.QueryRow("SELECT count(*) FROM evidence WHERE value='keep'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration lost or duplicated data: count=%d error=%v", count, err)
	}
	files["migrations/003_broken.sql"] = &fstest.MapFile{Data: []byte("DELETE FROM evidence; INVALID SQL;")}
	if err := migrate(database, files); err == nil {
		t.Fatal("invalid migration was accepted")
	}
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("failed migration advanced version: %d %v", version, err)
	}
	if err := database.QueryRow("SELECT count(*) FROM evidence").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed migration did not roll back: count=%d error=%v", count, err)
	}
	delete(files, "migrations/003_broken.sql")
	delete(files, "migrations/002_extend.sql")
	if err := migrate(database, files); err == nil {
		t.Fatal("older application accepted newer database schema")
	}
}

func TestOpenUpgradesUnversionedSupportDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	schema, err := migrations.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO tabs(id,name) VALUES('legacy-tab','Existing');
		INSERT INTO apps(id,tab_id,config,secret) VALUES('legacy-app','legacy-tab',
		'{"id":"legacy-app","name":"Before","tab_id":"legacy-tab","tags":[],"heartbeat_timeout_seconds":120,"retention_days":7}',zeroblob(32));
		INSERT INTO samples(app_id,stream,timestamp,received_at,value)
		VALUES('legacy-app','cpu','2026-10-26T00:00:00Z',1,'42');
	`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version, samples int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("legacy version: %d %v", version, err)
	}
	if err := store.db.QueryRow("SELECT count(*) FROM samples WHERE app_id='legacy-app' AND value='42'").Scan(&samples); err != nil || samples != 1 {
		t.Fatalf("legacy samples: %d %v", samples, err)
	}
	var tab string
	if err := store.db.QueryRow("SELECT name FROM tabs WHERE id='legacy-tab'").Scan(&tab); err != nil || tab != "Existing" {
		t.Fatalf("legacy tab: %q %v", tab, err)
	}
}
