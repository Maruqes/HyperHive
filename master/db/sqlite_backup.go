package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	logger "github.com/Maruqes/512SvMan/logger"
	sqlite3 "github.com/mattn/go-sqlite3"
	"golang.org/x/sys/unix"
)

const (
	sqliteDatabaseFile          = "data.db"
	sqliteBackupRetentionDays   = 14
	sqliteBackupInterval        = 15 * time.Minute
	sqliteBackupTimestampFormat = "2006-01-02_15-04-05" // Legacy filenames.
	sqliteBackupDateFormat      = "2006-01-02"
	sqliteBackupStepPages       = 256
	sqliteBackupRetryDelay      = 50 * time.Millisecond
	sqliteBackupTimeout         = 10 * time.Minute
	sqliteBackupMinHeadroom     = uint64(256 * 1024 * 1024)
)

// All state needed across restarts lives in the backup directory. Dependencies
// are injectable so tests exercise scheduling and failures without real delays.
type sqliteBackupRunner struct {
	dbPath     string
	src        *sql.DB
	now        func() time.Time
	copy       func(context.Context, *sql.DB, string) error
	checkSpace func(context.Context, *sql.DB, string) error
	remove     func(string) error
}

func newSQLiteBackupRunner(dbPath string, src *sql.DB) *sqliteBackupRunner {
	return &sqliteBackupRunner{
		dbPath: dbPath, src: src, now: time.Now,
		copy: backupSQLiteDB, checkSpace: checkSQLiteBackupSpace, remove: os.Remove,
	}
}

func StartSQLiteBackupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(sqliteBackupInterval)
		defer ticker.Stop()
		newSQLiteBackupRunner(sqliteDatabaseFile, DB).loop(ctx, ticker.C)
	}()
}

func (r *sqliteBackupRunner) loop(ctx context.Context, ticks <-chan time.Time) {
	var previous time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		now := r.now()
		if now.Before(previous) {
			logger.Warnf("sqlite backup: clock moved backwards from %s to %s", previous, now)
		}
		previous = now
		if err := r.reconcile(ctx, true); err != nil && ctx.Err() == nil {
			logger.Errorf("sqlite backup failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
		}
	}
}

func RunSQLiteBackup(ctx context.Context) error {
	return newSQLiteBackupRunner(sqliteDatabaseFile, DB).reconcile(ctx, true)
}

// CleanupSQLiteBackups needs no live database and must run before InitDB:
// otherwise a full disk can prevent startup from ever reaching cleanup.
func CleanupSQLiteBackups(ctx context.Context) error {
	return newSQLiteBackupRunner(sqliteDatabaseFile, nil).reconcile(ctx, false)
}

func (r *sqliteBackupRunner) reconcile(ctx context.Context, create bool) (retErr error) {
	ctx, cancel := context.WithTimeout(ctx, sqliteBackupTimeout)
	defer cancel()
	// Lock the existing directory, not a newly created lock file: cleanup must
	// work even when no disk blocks/inodes remain. flock is released on exit.
	dir, err := lockSQLiteBackupDirectory(ctx, filepath.Dir(r.dbPath))
	if err != nil {
		return err
	}
	defer dir.Close()
	defer func() { retErr = errors.Join(retErr, dir.Sync()) }()

	now := r.now() // One calendar date for this entire attempt, including publication.
	todayExists, err := r.prune(ctx, now, dir)
	if err != nil {
		return err // Do not add more files when cleanup failed.
	}
	if !create || todayExists {
		return nil
	}
	if r.src == nil {
		return errors.New("sqlite backup: db not initialized")
	}
	if err := r.checkSpace(ctx, r.src, filepath.Dir(r.dbPath)); err != nil {
		return err
	}

	finalPath := backupPathForDB(r.dbPath, now)
	if _, err := os.Lstat(finalPath); !os.IsNotExist(err) {
		return fmt.Errorf("sqlite backup: destination already exists or cannot be inspected: %s (%v)", finalPath, err)
	}
	tmp := r.dbPath + ".bak.in-progress"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, r.removeTemporaryFiles()) }()
	if err := file.Close(); err != nil {
		return err
	}
	if err := r.copy(ctx, r.src, tmp); err != nil {
		return err
	}
	if err := validateSQLiteBackup(ctx, tmp); err != nil {
		return fmt.Errorf("sqlite backup validation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := syncSQLiteBackupFile(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, finalPath); err != nil {
		return err
	}
	logger.Infof("sqlite backup created: %s (retention: %d local calendar days)", finalPath, sqliteBackupRetentionDays)
	return nil
}

func lockSQLiteBackupDirectory(ctx context.Context, path string) (*os.File, error) {
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			dir.Close()
			return nil, err
		}
		err = unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			dir.Close()
			return nil, err
		}
		if err := waitSQLiteBackup(ctx); err != nil {
			dir.Close()
			return nil, err
		}
	}
}

func backupPathForDB(dbPath string, ts time.Time) string {
	return dbPath + ".bak." + ts.Format(sqliteBackupDateFormat)
}

func (r *sqliteBackupRunner) removeTemporaryFiles() error {
	var errs []error
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		path := r.dbPath + ".bak.in-progress" + suffix
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
		} else if !info.Mode().IsRegular() {
			errs = append(errs, fmt.Errorf("sqlite backup: refusing non-regular temporary path %s", path))
		} else if err := r.remove(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

type sqliteBackupEntry struct {
	path string
	ts   time.Time
}

// Only exact managed filenames are eligible for deletion. In particular, do not
// use modification times to classify arbitrary .bak.* files as our backups.
func (r *sqliteBackupRunner) prune(ctx context.Context, now time.Time, dir *os.File) (bool, error) {
	entries, err := os.ReadDir(filepath.Dir(r.dbPath))
	if err != nil {
		return false, err
	}
	today := now.Format(sqliteBackupDateFormat)
	cutoff := now.AddDate(0, 0, -(sqliteBackupRetentionDays - 1)).Format(sqliteBackupDateFormat)
	prefix := filepath.Base(r.dbPath) + ".bak."
	groups := make(map[string][]sqliteBackupEntry)
	errs := []error{r.removeTemporaryFiles()}
	remove := func(path string) {
		if err := r.remove(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		} else {
			logger.Infof("sqlite backup pruned: %s", path)
		}
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, errors.Join(append(errs, err)...)
		}
		if !entry.Type().IsRegular() {
			continue
		}
		ts, ok := sqliteBackupCreatedAt(entry.Name(), prefix, now.Location())
		if !ok {
			if strings.HasPrefix(entry.Name(), prefix) && !strings.HasPrefix(entry.Name(), prefix+"in-progress") {
				logger.Warnf("sqlite backup: leaving unrecognized file untouched: %s", entry.Name())
			}
			continue
		}
		path := filepath.Join(filepath.Dir(r.dbPath), entry.Name())
		date := ts.Format(sqliteBackupDateFormat)
		if date < cutoff || date > today {
			remove(path)
		} else {
			groups[date] = append(groups[date], sqliteBackupEntry{path, ts})
		}
	}
	// Reclaim expired snapshots before reading/validating any legacy candidates.
	var todayExists bool
	for date, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].ts.After(group[j].ts) })
		canonical := backupPathForDB(r.dbPath, group[0].ts)
		keep := ""
		for _, entry := range group {
			if entry.path == canonical {
				keep = canonical // Published atomically by the new writer.
				break
			}
		}
		if keep == "" {
			for _, entry := range group {
				if err := validateSQLiteBackup(ctx, entry.path); err != nil {
					// Corruption is removable, but I/O, permission and cancellation
					// errors must not be mistaken for proof a snapshot is invalid.
					if !errors.Is(err, errInvalidSQLiteBackup) {
						return false, errors.Join(append(errs, err)...)
					}
					logger.Warnf("sqlite backup: invalid legacy snapshot %s: %v", entry.path, err)
					continue
				}
				keep = entry.path
				break
			}
			if keep != "" {
				if _, err := os.Lstat(canonical); !os.IsNotExist(err) {
					return false, fmt.Errorf("sqlite backup: refusing to replace %s", canonical)
				}
				if err := syncSQLiteBackupFile(keep); err != nil {
					return false, err
				}
				if err := os.Rename(keep, canonical); err != nil {
					return false, errors.Join(append(errs, err)...)
				}
				// Persist the chosen recovery point before deleting alternatives.
				if err := dir.Sync(); err != nil {
					return false, errors.Join(append(errs, err)...)
				}
			}
		}
		for _, entry := range group {
			if entry.path != keep {
				remove(entry.path)
			}
		}
		if date == today && keep != "" {
			todayExists = true
		}
	}
	return todayExists, errors.Join(errs...)
}

func sqliteBackupCreatedAt(name, prefix string, location *time.Location) (time.Time, bool) {
	if !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}
	value := strings.TrimPrefix(name, prefix)
	for _, format := range []string{sqliteBackupDateFormat, sqliteBackupTimestampFormat} {
		if ts, err := time.ParseInLocation(format, value, location); err == nil && ts.Format(format) == value {
			return ts, true
		}
	}
	return time.Time{}, false
}

func sqliteBackupDSN(path, mode string) string {
	absolute, _ := filepath.Abs(path)
	u := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{"mode": {mode}, "_busy_timeout": {"15000"}}
	u.RawQuery = query.Encode()
	return u.String()
}

var errInvalidSQLiteBackup = errors.New("invalid SQLite backup")

func validateSQLiteBackup(ctx context.Context, path string) (retErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < 100 {
		return fmt.Errorf("%w: missing database header", errInvalidSQLiteBackup)
	}
	// Snapshots are standalone and never changed while the directory is locked.
	// immutable avoids creating WAL/SHM files when inspecting legacy snapshots.
	db, err := sql.Open("sqlite3", sqliteBackupDSN(path, "ro")+"&immutable=1")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, db.Close()) }()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrNotADB) {
			return fmt.Errorf("%w: %v", errInvalidSQLiteBackup, err)
		}
		return err
	}
	if result != "ok" {
		return fmt.Errorf("%w: %s", errInvalidSQLiteBackup, result)
	}
	return nil
}

func syncSQLiteBackupFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = file.Sync()
	return errors.Join(err, file.Close())
}

func checkSQLiteBackupSpace(ctx context.Context, src *sql.DB, dir string) error {
	var pages, pageSize uint64
	if err := src.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return err
	}
	if err := src.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return err
	}
	return sqliteBackupSpaceError(pages*pageSize, stat.Bavail*uint64(stat.Bsize))
}

func sqliteBackupSpaceError(size, available uint64) error {
	margin := max(sqliteBackupMinHeadroom, size/10)
	if available < size+margin {
		return fmt.Errorf("sqlite backup: insufficient free space: have %d bytes, need %d (snapshot plus headroom)", available, size+margin)
	}
	return nil
}

func backupSQLiteDB(ctx context.Context, src *sql.DB, backupPath string) (retErr error) {
	destDB, err := sql.Open("sqlite3", sqliteBackupDSN(backupPath, "rw"))
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, destDB.Close()) }()
	srcConn, err := src.Conn(ctx)
	if err != nil {
		return err
	}
	defer srcConn.Close()
	// SQLite's busy handler runs inside C and cannot observe ctx. Keep each
	// backup step short and retry in Go, then restore this pooled connection's
	// application timeout before returning it to normal database queries.
	var busyTimeout int
	if err := srcConn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return err
	}
	if _, err := srcConn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", sqliteBackupRetryDelay.Milliseconds())); err != nil {
		return err
	}
	defer func() {
		_, err := srcConn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout))
		retErr = errors.Join(retErr, err)
	}()
	destConn, err := destDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, destConn.Close()) }()
	err = destConn.Raw(func(destDriver any) error {
		destSQLite, ok := destDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("sqlite backup: unexpected dest conn type %T", destDriver)
		}
		return srcConn.Raw(func(srcDriver any) (backupErr error) {
			srcSQLite, ok := srcDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("sqlite backup: unexpected src conn type %T", srcDriver)
			}
			backup, err := destSQLite.Backup("main", srcSQLite, "main")
			if err != nil {
				return err
			}
			defer func() { backupErr = errors.Join(backupErr, backup.Finish()) }()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				done, err := backup.Step(sqliteBackupStepPages)
				if err != nil || done {
					return err
				}
				if err := waitSQLiteBackup(ctx); err != nil {
					return err
				}
			}
		})
	})
	if err != nil {
		return err
	}
	// Publish a self-contained database without depending on temporary sidecars.
	_, err = destConn.ExecContext(ctx, "PRAGMA journal_mode=DELETE")
	return err
}

func waitSQLiteBackup(ctx context.Context) error {
	timer := time.NewTimer(sqliteBackupRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
