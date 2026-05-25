package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneSQLiteBackupsDeletesOnlyBackupsOlderThanRetention(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Base(sqliteDatabaseFile) + ".bak."
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.Local)

	oldBackup := mustCreateSQLiteBackupFile(t, dir, now.AddDate(0, -1, -1))
	cutoffBackup := mustCreateSQLiteBackupFile(t, dir, now.AddDate(0, -1, 0))
	recentBackup := mustCreateSQLiteBackupFile(t, dir, now.AddDate(0, 0, -7))

	if err := pruneSQLiteBackups(dir, prefix, 1, now); err != nil {
		t.Fatalf("pruneSQLiteBackups returned error: %v", err)
	}

	assertNotExists(t, oldBackup)
	assertExists(t, cutoffBackup)
	assertExists(t, recentBackup)
}

func TestPruneSQLiteBackupsFallsBackToModTimeForUnexpectedNames(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Base(sqliteDatabaseFile) + ".bak."
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.Local)
	path := filepath.Join(dir, prefix+"manual-copy")

	if err := os.WriteFile(path, []byte("backup"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	old := now.AddDate(0, -2, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("set backup mtime: %v", err)
	}

	if err := pruneSQLiteBackups(dir, prefix, 1, now); err != nil {
		t.Fatalf("pruneSQLiteBackups returned error: %v", err)
	}

	assertNotExists(t, path)
}

func mustCreateSQLiteBackupFile(t *testing.T, dir string, ts time.Time) string {
	t.Helper()

	path := backupPathForDB(filepath.Join(dir, sqliteDatabaseFile), ts)
	if err := os.WriteFile(path, []byte("backup"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return path
}

func assertExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err: %v", path, err)
	}
}
