package db

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func snapshotFixture(t *testing.T) []byte {
	t.Helper()
	return snapshotFixtureValue(t, "recoverable")
}

func snapshotFixtureValue(t *testing.T, value string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite3", sqliteBackupDSN(path, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE example (value TEXT); INSERT INTO example VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeSnapshot(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testBackupRunner(t *testing.T, path string, now time.Time, data []byte) *sqliteBackupRunner {
	t.Helper()
	r := newSQLiteBackupRunner(path, &sql.DB{})
	r.now = func() time.Time { return now }
	r.copy = func(_ context.Context, _ *sql.DB, dest string) error { return os.WriteFile(dest, data, 0o600) }
	r.checkSpace = func(context.Context, *sql.DB, string) error { return nil }
	return r
}

func backupNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if _, ok := sqliteBackupCreatedAt(entry.Name(), filepath.Base(path)+".bak.", time.Local); ok {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func assertBackupDates(t *testing.T, path string, now time.Time, count int) {
	t.Helper()
	var want []string
	for i := count - 1; i >= 0; i-- {
		want = append(want, filepath.Base(backupPathForDB(path, now.AddDate(0, 0, -i))))
	}
	if got := backupNames(t, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("backups:\n got %v\nwant %v", got, want)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s absent, got %v", path, err)
	}
}

func TestSQLiteBackupDailyRestartsFor31Days(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	start := time.Date(2026, 12, 20, 23, 55, 0, 0, time.Local)
	for day := 0; day < 31; day++ {
		now := start.AddDate(0, 0, day)
		for restart := 0; restart < 4; restart++ {
			// New runner each time: nothing except files survives a restart.
			r := testBackupRunner(t, path, now, data)
			if err := r.reconcile(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			assertBackupDates(t, path, now, min(day+1, 14))
		}
	}
}

func TestSQLiteBackupContinuousSchedulerFor31Days(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	start := time.Date(2026, 9, 22, 23, 45, 0, 0, time.Local)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := make(chan time.Time)
	ticks := make(chan time.Time)
	r := testBackupRunner(t, path, start, data)
	r.now = func() time.Time {
		select {
		case now := <-clock:
			return now
		case <-ctx.Done():
			return start
		}
	}
	var copies atomic.Int32
	copyDB := r.copy
	r.copy = func(ctx context.Context, db *sql.DB, dest string) error { copies.Add(1); return copyDB(ctx, db, dest) }
	done := make(chan struct{})
	go func() { defer close(done); r.loop(ctx, ticks) }()
	// Drive every 15-minute pass of the real scheduler for 31 calendar days.
	for day := 0; day < 31; day++ {
		for pass := 0; pass < 96; pass++ {
			now := time.Date(start.Year(), start.Month(), start.Day()+day, 0, pass*15, 0, 0, start.Location())
			for call := 0; call < 2; call++ {
				select {
				case clock <- now:
				case <-time.After(5 * time.Second):
					t.Fatal("scheduler did not read clock")
				}
			}
			// Receipt means the preceding reconciliation finished; the next
			// pass blocks on our clock, so filesystem assertions are race-free.
			select {
			case ticks <- now:
			case <-time.After(5 * time.Second):
				t.Fatal("scheduler did not complete")
			}
			assertBackupDates(t, path, now, min(day+1, 14))
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler ignored cancellation")
	}
	if got := copies.Load(); got != 31 {
		t.Fatalf("got %d copies, want 31", got)
	}
}

func TestSQLiteBackupCalendarBoundariesAndDowntime(t *testing.T) {
	location, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	data := snapshotFixture(t)
	for _, now := range []time.Time{
		time.Date(2026, 3, 30, 0, 1, 0, 0, location),    // DST spring, 23-hour day.
		time.Date(2026, 10, 26, 23, 59, 0, 0, location), // DST fall, 25-hour day.
		time.Date(2028, 3, 1, 0, 0, 0, 0, location),     // Leap day.
		time.Date(2027, 1, 1, 0, 0, 0, 0, location),
	} {
		t.Run(now.Format(sqliteBackupDateFormat), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.db")
			for i := -2; i <= 30; i++ {
				writeSnapshot(t, backupPathForDB(path, now.AddDate(0, 0, -i)), data)
			}
			r := testBackupRunner(t, path, now, data)
			if err := r.reconcile(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			assertBackupDates(t, path, now, 14)
			later := now.AddDate(0, 0, 20)
			r.now = func() time.Time { return later }
			if err := r.reconcile(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			assertBackupDates(t, path, later, 1)
		})
	}
}

func TestSQLiteBackupMigratesHundredsOfLegacyDuplicates(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.Local)
	for day := 0; day < 35; day++ {
		for hour := 0; hour < 10; hour++ {
			ts := now.AddDate(0, 0, -day).Add(time.Duration(hour) * time.Hour)
			writeSnapshot(t, path+".bak."+ts.Format(sqliteBackupTimestampFormat), data)
		}
	}
	// Distinct contents ensure we retain the newest valid snapshot, not just
	// an arbitrary file from today's duplicates.
	latest := path + ".bak." + now.Add(9*time.Hour).Format(sqliteBackupTimestampFormat)
	writeSnapshot(t, latest, snapshotFixtureValue(t, "latest valid"))
	// The newest candidate is broken; migration must keep the valid fallback.
	bad := path + ".bak." + now.Add(11*time.Hour).Format(sqliteBackupTimestampFormat)
	writeSnapshot(t, bad, []byte("partial"))
	r := testBackupRunner(t, path, now, data)
	r.copy = func(context.Context, *sql.DB, string) error {
		t.Fatal("legacy snapshot should satisfy today")
		return nil
	}
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertBackupDates(t, path, now, 14)
	for _, name := range backupNames(t, path) {
		if err := validateSQLiteBackup(context.Background(), filepath.Join(filepath.Dir(path), name)); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := sql.Open("sqlite3", sqliteBackupDSN(backupPathForDB(path, now), "ro")+"&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM example").Scan(&value); err != nil || value != "latest valid" {
		t.Fatalf("retained wrong legacy snapshot: %q, %v", value, err)
	}
	// A subsequent restart sees canonical names and remains idempotent.
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertBackupDates(t, path, now, 14)
}

func TestSQLiteBackupPreservesUnmanagedFiles(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Now()
	untouched := []string{path, path + "-wal", path + "-shm", path + ".bak.manual-copy", path + ".bak.2026-99-99", path + ".bak.in-progress-other"}
	for _, name := range untouched {
		writeSnapshot(t, name, []byte("untouched"))
	}
	link := backupPathForDB(path, now.AddDate(0, 0, -20))
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	directory := backupPathForDB(path, now.AddDate(0, 0, -21))
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r := testBackupRunner(t, path, now, data)
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for _, name := range untouched {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != "untouched" {
			t.Fatalf("modified %s: %q, %v", name, got, err)
		}
	}
	for _, name := range []string{link, directory} {
		if _, err := os.Lstat(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteBackupCleansBeforeFailuresAndRetries(t *testing.T) {
	data := snapshotFixture(t)
	for _, failure := range []string{"space", "copy", "validation", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.db")
			now := time.Now()
			old := backupPathForDB(path, now.AddDate(0, 0, -14))
			writeSnapshot(t, old, data)
			yesterday := backupPathForDB(path, now.AddDate(0, 0, -1))
			writeSnapshot(t, yesterday, data)
			r := testBackupRunner(t, path, now, data)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "space" {
				r.checkSpace = func(context.Context, *sql.DB, string) error { assertNotExists(t, old); return syscall.ENOSPC }
			} else {
				r.copy = func(_ context.Context, _ *sql.DB, dest string) error {
					assertNotExists(t, old)
					writeSnapshot(t, dest, []byte("partial"))
					writeSnapshot(t, dest+"-journal", []byte("journal"))
					if failure == "cancel" {
						cancel()
						return ctx.Err()
					}
					if failure == "copy" {
						return syscall.ENOSPC
					}
					return nil // Validation must reject a silently incomplete copy.
				}
			}
			if err := r.reconcile(ctx, true); err == nil {
				t.Fatal("wanted backup failure")
			}
			assertNotExists(t, old)
			assertNotExists(t, backupPathForDB(path, now))
			assertNotExists(t, path+".bak.in-progress")
			assertNotExists(t, path+".bak.in-progress-journal")
			if got, err := os.ReadFile(yesterday); err != nil || !reflect.DeepEqual(got, data) {
				t.Fatal("lost retained backup")
			}
			if err := testBackupRunner(t, path, now, data).reconcile(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			assertBackupDates(t, path, now, 2)
		})
	}
}

func TestSQLiteBackupCleanupFailurePreventsGrowth(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Now()
	old := backupPathForDB(path, now.AddDate(0, 0, -14))
	writeSnapshot(t, old, data)
	r := testBackupRunner(t, path, now, data)
	r.remove = func(path string) error { return &os.PathError{Op: "remove", Path: path, Err: syscall.EACCES} }
	r.copy = func(context.Context, *sql.DB, string) error { t.Fatal("copy after failed cleanup"); return nil }
	for attempt := 0; attempt < 4; attempt++ {
		if err := r.reconcile(context.Background(), true); !errors.Is(err, syscall.EACCES) {
			t.Fatalf("got %v", err)
		}
		assertNotExists(t, backupPathForDB(path, now))
		if got := len(backupNames(t, path)); got != 1 {
			t.Fatalf("grew to %d files", got)
		}
	}
}

func TestSQLiteBackupStartupCleanupWithoutDatabase(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Now()
	old := backupPathForDB(path, now.AddDate(0, 0, -30))
	writeSnapshot(t, old, data)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		writeSnapshot(t, path+".bak.in-progress"+suffix, []byte("crash"))
	}
	r := newSQLiteBackupRunner(path, nil)
	if err := r.reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	assertNotExists(t, old)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatalf("leftover files: %v, %v", entries, err)
	}
}

func TestSQLiteBackupConcurrentRunners(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Now()
	var copies atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := testBackupRunner(t, path, now, data)
			copyDB := r.copy
			r.copy = func(ctx context.Context, db *sql.DB, dest string) error { copies.Add(1); return copyDB(ctx, db, dest) }
			errs <- r.reconcile(context.Background(), true)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := copies.Load(); got != 1 {
		t.Fatalf("%d copies, want 1", got)
	}
	assertBackupDates(t, path, now, 1)
}

func TestSQLiteBackupRealWALSnapshot(t *testing.T) {
	// URI-sensitive characters must not alter DSN options or the destination.
	path := filepath.Join(t.TempDir(), "data ?#%.db")
	src, err := sql.Open("sqlite3", sqliteBackupDSN(path, "rwc")+"&_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Exec("PRAGMA wal_autocheckpoint=0; CREATE TABLE example (value TEXT); INSERT INTO example VALUES ('committed in WAL')"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + "-wal")
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected live WAL: %v", err)
	}
	r := newSQLiteBackupRunner(path, src)
	now := time.Now()
	r.now = func() time.Time { return now }
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	backup := backupPathForDB(path, now)
	if err := validateSQLiteBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite3", sqliteBackupDSN(backup, "ro")+"&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM example").Scan(&value); err != nil || value != "committed in WAL" {
		t.Fatalf("restore: %q, %v", value, err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		assertNotExists(t, path+".bak.in-progress"+suffix)
	}
	if _, err := src.Exec("INSERT INTO example VALUES ('after backup')"); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := restored.QueryRow("SELECT count(*) FROM example").Scan(&count); err != nil || count != 1 {
		t.Fatalf("same-day backup changed: %d, %v", count, err)
	}
}

func TestSQLiteBackupProcessCrashRecovery(t *testing.T) {
	if dir := os.Getenv("HYPERHIVE_BACKUP_TEST_DIR"); dir != "" {
		lock, err := lockSQLiteBackupDirectory(context.Background(), dir)
		if err != nil {
			panic(err)
		}
		defer lock.Close()
		path := filepath.Join(dir, "data.db")
		if os.Getenv("HYPERHIVE_BACKUP_TEST_PUBLISHED") == "yes" {
			data, err := os.ReadFile(filepath.Join(dir, "fixture"))
			if err != nil {
				panic(err)
			}
			if err := os.WriteFile(backupPathForDB(path, time.Now()), data, 0o600); err != nil {
				panic(err)
			}
		} else {
			if err := os.WriteFile(path+".bak.in-progress", []byte("interrupted"), 0o600); err != nil {
				panic(err)
			}
		}
		fmt.Println("ready")
		time.Sleep(time.Hour) // Parent kills us while we own the filesystem lock.
		return
	}
	data := snapshotFixture(t)
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data.db")
			writeSnapshot(t, filepath.Join(dir, "fixture"), data)
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteBackupProcessCrashRecovery$")
			cmd.Env = append(os.Environ(), "HYPERHIVE_BACKUP_TEST_DIR="+dir)
			if published {
				cmd.Env = append(cmd.Env, "HYPERHIVE_BACKUP_TEST_PUBLISHED=yes")
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cmd.Process.Kill() })
			ready := make(chan string, 1)
			go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
			select {
			case line := <-ready:
				if strings.TrimSpace(line) != "ready" {
					t.Fatalf("helper: %q", line)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("helper timed out")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if lock, err := lockSQLiteBackupDirectory(ctx, dir); err == nil {
				lock.Close()
				t.Fatal("another process acquired active lock")
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			r := testBackupRunner(t, path, time.Now(), data)
			if published {
				r.copy = func(context.Context, *sql.DB, string) error { t.Fatal("copied again after publication"); return nil }
			}
			if err := r.reconcile(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			assertBackupDates(t, path, time.Now(), 1)
			assertNotExists(t, path+".bak.in-progress")
		})
	}
}

func TestSQLiteBackupReservedSymlinkDoesNotOverwriteTarget(t *testing.T) {
	data := snapshotFixture(t)
	for _, suffix := range []string{".bak.in-progress", ".bak." + time.Now().Format(sqliteBackupDateFormat)} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.db")
			writeSnapshot(t, path, data)
			if err := os.Symlink(path, path+suffix); err != nil {
				t.Fatal(err)
			}
			if err := testBackupRunner(t, path, time.Now(), data).reconcile(context.Background(), true); err == nil {
				t.Fatal("expected collision error")
			}
			got, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(got, data) {
				t.Fatal("live database was changed")
			}
		})
	}
}

func TestSQLiteBackupHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name            string
		size, available uint64
		wantError       bool
	}{
		{"empty disk", 4096, 0, true},
		{"copy fits but reserve does not", 4096, 4096, true},
		{"minimum reserve boundary", 4096, 4096 + 256*1024*1024, false},
		{"one byte below reserve", 4096, 4096 + 256*1024*1024 - 1, true},
		{"large database needs ten percent", 10_000_000_000, 10_500_000_000, true},
		{"large database sufficient reserve", 10_000_000_000, 11_000_000_000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := sqliteBackupSpaceError(tc.size, tc.available); (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSQLiteBackupCrossesMidnightDuringCopy(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Date(2026, 9, 22, 23, 59, 59, 0, time.Local)
	started := now
	r := testBackupRunner(t, path, now, data)
	r.now = func() time.Time { return now }
	copyDB := r.copy
	r.copy = func(ctx context.Context, db *sql.DB, dest string) error {
		now = now.Add(2 * time.Second)
		return copyDB(ctx, db, dest)
	}
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertBackupDates(t, path, started, 1)
	if err := r.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertBackupDates(t, path, now, 2)
}

func TestSQLiteBackupRejectsCorruptLegacyWithoutSuppressingToday(t *testing.T) {
	data := snapshotFixture(t)
	path := filepath.Join(t.TempDir(), "data.db")
	now := time.Now()
	bad := path + ".bak." + now.Format(sqliteBackupTimestampFormat)
	writeSnapshot(t, bad, []byte(strings.Repeat("not a database", 100)))
	if err := testBackupRunner(t, path, now, data).reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	assertNotExists(t, bad)
	assertBackupDates(t, path, now, 1)
	if err := validateSQLiteBackup(context.Background(), backupPathForDB(path, now)); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteBackupCancellationWhileSourceLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	src, err := sql.Open("sqlite3", sqliteBackupDSN(path, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Exec("CREATE TABLE example (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	writer, err := src.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	// Warm the read connection before taking the exclusive write lock. This
	// tests backup-step cancellation rather than SQLite connection startup.
	reader, err := src.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer writer.ExecContext(context.Background(), "ROLLBACK")
	tmp := path + ".bak.in-progress"
	writeSnapshot(t, tmp, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := backupSQLiteDB(ctx, src, tmp); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not interrupt retry wait promptly")
	}
	var timeout int
	if err := src.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 15000 {
		t.Fatalf("application connection timeout was not restored: %d, %v", timeout, err)
	}
}
