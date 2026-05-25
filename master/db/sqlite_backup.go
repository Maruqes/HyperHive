package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	logger "github.com/Maruqes/512SvMan/logger"
	sqlite3 "github.com/mattn/go-sqlite3"
)

const (
	sqliteDatabaseFile          = "data.db"
	sqliteBackupRetentionMonths = 1
	sqliteBackupInterval        = 24 * time.Hour
	sqliteBackupTimestampFormat = "2006-01-02_15-04-05"
	sqliteBackupStepPages       = 256
	sqliteBackupRetryDelay      = 50 * time.Millisecond
)

func StartSQLiteBackupLoop(ctx context.Context) {
	go func() {
		if err := RunSQLiteBackup(ctx); err != nil {
			logger.Errorf("sqlite backup failed: %v", err)
		}

		ticker := time.NewTicker(sqliteBackupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := RunSQLiteBackup(ctx); err != nil {
					logger.Errorf("sqlite backup failed: %v", err)
				}
			}
		}
	}()
}

func RunSQLiteBackup(ctx context.Context) error {
	if DB == nil {
		return errors.New("sqlite backup: db not initialized")
	}

	backupPath := backupPathForDB(sqliteDatabaseFile, time.Now())
	if err := backupSQLiteDB(ctx, DB, backupPath); err != nil {
		return err
	}
	if err := pruneSQLiteBackups(filepath.Dir(backupPath), filepath.Base(sqliteDatabaseFile)+".bak.", sqliteBackupRetentionMonths, time.Now()); err != nil {
		return err
	}

	logger.Infof("sqlite backup created: %s", backupPath)
	return nil
}

func backupPathForDB(dbPath string, ts time.Time) string {
	base := filepath.Base(dbPath)
	dir := filepath.Dir(dbPath)
	name := fmt.Sprintf("%s.bak.%s", base, ts.Format(sqliteBackupTimestampFormat))
	return filepath.Join(dir, name)
}

func backupSQLiteDB(ctx context.Context, src *sql.DB, backupPath string) error {
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}

	destDsn := fmt.Sprintf("file:%s?mode=rwc&_busy_timeout=15000", backupPath)
	destDB, err := sql.Open("sqlite3", destDsn)
	if err != nil {
		return err
	}
	defer destDB.Close()

	srcConn, err := src.Conn(ctx)
	if err != nil {
		return err
	}
	defer srcConn.Close()

	destConn, err := destDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer destConn.Close()

	return destConn.Raw(func(destDriver any) error {
		destSQLite, ok := destDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("sqlite backup: unexpected dest conn type %T", destDriver)
		}
		return srcConn.Raw(func(srcDriver any) error {
			srcSQLite, ok := srcDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("sqlite backup: unexpected src conn type %T", srcDriver)
			}
			backup, err := destSQLite.Backup("main", srcSQLite, "main")
			if err != nil {
				return err
			}
			defer backup.Close()

			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				done, err := backup.Step(sqliteBackupStepPages)
				if err != nil {
					return err
				}
				if done {
					break
				}
				time.Sleep(sqliteBackupRetryDelay)
			}
			return nil
		})
	})
}

func pruneSQLiteBackups(dir, prefix string, retentionMonths int, now time.Time) error {
	if retentionMonths < 1 {
		return fmt.Errorf("sqlite backup retention months must be at least 1")
	}

	pattern := filepath.Join(dir, prefix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}

	type entry struct {
		path      string
		createdAt time.Time
	}
	entries := make([]entry, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		createdAt, ok := sqliteBackupCreatedAt(filepath.Base(match), prefix)
		if !ok {
			createdAt = info.ModTime()
		}
		entries = append(entries, entry{path: match, createdAt: createdAt})
	}
	if len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].createdAt.After(entries[j].createdAt)
	})

	cutoff := now.AddDate(0, -retentionMonths, 0)
	var lastErr error
	for _, entry := range entries {
		if !entry.createdAt.Before(cutoff) {
			continue
		}
		if err := os.Remove(entry.path); err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("sqlite backup cleanup failed: %w", lastErr)
	}
	return nil
}

func sqliteBackupCreatedAt(name, prefix string) (time.Time, bool) {
	if !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}
	ts, err := time.ParseInLocation(sqliteBackupTimestampFormat, name[len(prefix):], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
