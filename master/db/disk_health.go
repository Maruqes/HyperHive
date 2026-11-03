package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"512SvMan/diskhealth"
)

// DiskHealthDB holds SMART history in its own file. It is not part of the
// SQLite backup because the data is non-critical; it is nil when the file
// could not be opened, which disables the disk health feature.
var DiskHealthDB *sql.DB

const diskHealthDatabaseFile = "disk_health.db"

// diskHealthTimeLayout is fixed-width so stored times sort as strings.
const diskHealthTimeLayout = "2006-01-02T15:04:05.000000000Z"

var (
	ErrDiskHealthDisabled = errors.New("disk health database is not available")
	ErrDiskNotFound       = errors.New("disk not found")
	ErrDiskNotRetired     = errors.New("disk is not retired")
)

// DiskHealthDisk is one physical disk, keyed by serial.
type DiskHealthDisk struct {
	Serial         string     `json:"serial"`
	Model          string     `json:"model"`
	MachineName    string     `json:"machineName"`
	Device         string     `json:"device"`
	Transport      string     `json:"transport"`
	Rotational     bool       `json:"rotational"`
	CapacityBytes  int64      `json:"capacityBytes"`
	FirstSeen      time.Time  `json:"firstSeen"`
	LastSeen       time.Time  `json:"lastSeen"`
	RetiredAt      *time.Time `json:"retiredAt"`
	SmartAvailable bool       `json:"smartAvailable"`
	Score          *int       `json:"score"`
	Band           string     `json:"band"`
	Reason         string     `json:"-"`
	WearOutAt      *time.Time `json:"wearOutAt"`
	AlertBand      string     `json:"-"`
	AlertCounters  string     `json:"-"`
}

// DiskSeen is what one poll learned about a disk's identity and location.
type DiskSeen struct {
	Serial        string
	Model         string
	MachineName   string
	Device        string
	Transport     string
	Rotational    bool
	CapacityBytes int64
	SeenAt        time.Time
}

// DiskScoreUpdate is the latest scoring result of a disk.
type DiskScoreUpdate struct {
	SmartAvailable bool
	Score          *int
	Band           string
	Reason         string
	WearOutAt      *time.Time
}

// DiskHealthSample is one raw 6-hourly SMART reading.
type DiskHealthSample struct {
	ID                      int64     `json:"-"`
	Serial                  string    `json:"-"`
	CapturedAt              time.Time `json:"capturedAt"`
	MachineName             string    `json:"machineName"`
	Device                  string    `json:"device"`
	Score                   *int      `json:"score"`
	TemperatureC            int64     `json:"temperatureC"`
	PowerOnHours            int64     `json:"powerOnHours"`
	PendingSectors          int64     `json:"pendingSectors"`
	OfflineUncorrectable    int64     `json:"offlineUncorrectable"`
	ReallocatedSectors      int64     `json:"reallocatedSectors"`
	ReportedUncorrectable   int64     `json:"reportedUncorrectable"`
	MediaErrors             int64     `json:"mediaErrors"`
	CRCErrorCount           int64     `json:"crcErrorCount"`
	PercentageUsed          int64     `json:"percentageUsed"`
	AvailableSpare          int64     `json:"availableSpare"`
	AvailableSpareThreshold int64     `json:"availableSpareThreshold"`
	CriticalWarning         int64     `json:"criticalWarning"`
	SmartPassed             bool      `json:"smartPassed"`
	SelfTestFailed          bool      `json:"selfTestFailed"`
	HealthStatus            string    `json:"healthStatus"`
	SmartctlError           string    `json:"smartctlError"`
}

// DiskHealthDaily is one compacted day of a disk: the worst score, the highest
// counters and temperature, and the last wear, power-on and SMART verdict.
type DiskHealthDaily struct {
	Serial                string `json:"-"`
	Day                   string `json:"day"`
	Score                 *int   `json:"score"`
	MaxTemperatureC       int64  `json:"maxTemperatureC"`
	PowerOnHours          int64  `json:"powerOnHours"`
	PendingSectors        int64  `json:"pendingSectors"`
	OfflineUncorrectable  int64  `json:"offlineUncorrectable"`
	ReallocatedSectors    int64  `json:"reallocatedSectors"`
	ReportedUncorrectable int64  `json:"reportedUncorrectable"`
	MediaErrors           int64  `json:"mediaErrors"`
	CRCErrorCount         int64  `json:"crcErrorCount"`
	PercentageUsed        int64  `json:"percentageUsed"`
	SmartPassed           bool   `json:"smartPassed"`
}

// InitDiskHealthDB opens disk_health.db next to data.db and creates its tables.
// On failure DiskHealthDB stays nil and the caller should run without the feature.
func InitDiskHealthDB(ctx context.Context) error {
	dsn := "file:" + diskHealthDatabaseFile + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=15000&_foreign_keys=on"
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", diskHealthDatabaseFile, err)
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("ping %s: %w", diskHealthDatabaseFile, err)
	}
	DiskHealthDB = conn
	if err := CreateDiskHealthTables(ctx); err != nil {
		DiskHealthDB = nil
		conn.Close()
		return err
	}
	return nil
}

func CreateDiskHealthTables(ctx context.Context) error {
	if DiskHealthDB == nil {
		return ErrDiskHealthDisabled
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS disk_health_disks (
			serial TEXT PRIMARY KEY,
			model TEXT NOT NULL DEFAULT '',
			machine_name TEXT NOT NULL DEFAULT '',
			device TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			rotational INTEGER NOT NULL DEFAULT 0,
			capacity_bytes INTEGER NOT NULL DEFAULT 0,
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			retired_at TEXT,
			smart_available INTEGER NOT NULL DEFAULT 0,
			score INTEGER,
			band TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			wear_out_at TEXT,
			alert_band TEXT NOT NULL DEFAULT '',
			alert_counters TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS disk_health_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			serial TEXT NOT NULL,
			captured_at TEXT NOT NULL,
			machine_name TEXT NOT NULL DEFAULT '',
			device TEXT NOT NULL DEFAULT '',
			score INTEGER,
			temperature_c INTEGER NOT NULL DEFAULT 0,
			power_on_hours INTEGER NOT NULL DEFAULT 0,
			pending_sectors INTEGER NOT NULL DEFAULT 0,
			offline_uncorrectable INTEGER NOT NULL DEFAULT 0,
			reallocated_sectors INTEGER NOT NULL DEFAULT 0,
			reported_uncorrectable INTEGER NOT NULL DEFAULT 0,
			media_errors INTEGER NOT NULL DEFAULT 0,
			crc_error_count INTEGER NOT NULL DEFAULT 0,
			percentage_used INTEGER NOT NULL DEFAULT 0,
			available_spare INTEGER NOT NULL DEFAULT 0,
			available_spare_threshold INTEGER NOT NULL DEFAULT 0,
			critical_warning INTEGER NOT NULL DEFAULT 0,
			smart_passed INTEGER NOT NULL DEFAULT 1,
			self_test_failed INTEGER NOT NULL DEFAULT 0,
			health_status TEXT NOT NULL DEFAULT '',
			smartctl_error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_disk_health_samples_serial_time ON disk_health_samples(serial, captured_at)`,
		`CREATE TABLE IF NOT EXISTS disk_health_daily (
			serial TEXT NOT NULL,
			day TEXT NOT NULL,
			score INTEGER,
			max_temperature_c INTEGER NOT NULL DEFAULT 0,
			power_on_hours INTEGER NOT NULL DEFAULT 0,
			pending_sectors INTEGER NOT NULL DEFAULT 0,
			offline_uncorrectable INTEGER NOT NULL DEFAULT 0,
			reallocated_sectors INTEGER NOT NULL DEFAULT 0,
			reported_uncorrectable INTEGER NOT NULL DEFAULT 0,
			media_errors INTEGER NOT NULL DEFAULT 0,
			crc_error_count INTEGER NOT NULL DEFAULT 0,
			percentage_used INTEGER NOT NULL DEFAULT 0,
			smart_passed INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (serial, day)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := DiskHealthDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create disk health tables: %w", err)
		}
	}
	return nil
}

func diskHealthDB() (*sql.DB, error) {
	if DiskHealthDB == nil {
		return nil, ErrDiskHealthDisabled
	}
	return DiskHealthDB, nil
}

func formatDiskHealthTime(t time.Time) string {
	return t.UTC().Format(diskHealthTimeLayout)
}

func parseDiskHealthTime(v string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, v)
}

func nullDiskHealthTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatDiskHealthTime(*t)
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func scanNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := parseDiskHealthTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanNullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// UpsertDiskSeen records that a disk was seen now. It refreshes the location
// and clears retired_at, so a retired disk that comes back is active again.
func UpsertDiskSeen(ctx context.Context, d DiskSeen) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	seen := formatDiskHealthTime(d.SeenAt)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO disk_health_disks (serial, model, machine_name, device, transport, rotational, capacity_bytes, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			model = excluded.model,
			machine_name = excluded.machine_name,
			device = excluded.device,
			transport = excluded.transport,
			rotational = excluded.rotational,
			capacity_bytes = excluded.capacity_bytes,
			last_seen = excluded.last_seen,
			retired_at = NULL`,
		d.Serial, d.Model, d.MachineName, d.Device, d.Transport, d.Rotational, d.CapacityBytes, seen, seen)
	if err != nil {
		return fmt.Errorf("upsert disk %s: %w", d.Serial, err)
	}
	return nil
}

func InsertDiskSample(ctx context.Context, s DiskHealthSample) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `
		INSERT INTO disk_health_samples (serial, captured_at, machine_name, device, score, temperature_c, power_on_hours,
			pending_sectors, offline_uncorrectable, reallocated_sectors, reported_uncorrectable, media_errors, crc_error_count,
			percentage_used, available_spare, available_spare_threshold, critical_warning, smart_passed, self_test_failed,
			health_status, smartctl_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Serial, formatDiskHealthTime(s.CapturedAt), s.MachineName, s.Device, nullInt(s.Score), s.TemperatureC, s.PowerOnHours,
		s.PendingSectors, s.OfflineUncorrectable, s.ReallocatedSectors, s.ReportedUncorrectable, s.MediaErrors, s.CRCErrorCount,
		s.PercentageUsed, s.AvailableSpare, s.AvailableSpareThreshold, s.CriticalWarning, s.SmartPassed, s.SelfTestFailed,
		s.HealthStatus, s.SmartctlError)
	if err != nil {
		return fmt.Errorf("insert disk sample %s: %w", s.Serial, err)
	}
	return nil
}

const diskSampleColumns = `id, serial, captured_at, machine_name, device, score, temperature_c, power_on_hours,
	pending_sectors, offline_uncorrectable, reallocated_sectors, reported_uncorrectable, media_errors, crc_error_count,
	percentage_used, available_spare, available_spare_threshold, critical_warning, smart_passed, self_test_failed,
	health_status, smartctl_error`

func scanDiskSample(rows interface{ Scan(...any) error }) (DiskHealthSample, error) {
	var s DiskHealthSample
	var captured string
	var score sql.NullInt64
	err := rows.Scan(&s.ID, &s.Serial, &captured, &s.MachineName, &s.Device, &score, &s.TemperatureC, &s.PowerOnHours,
		&s.PendingSectors, &s.OfflineUncorrectable, &s.ReallocatedSectors, &s.ReportedUncorrectable, &s.MediaErrors, &s.CRCErrorCount,
		&s.PercentageUsed, &s.AvailableSpare, &s.AvailableSpareThreshold, &s.CriticalWarning, &s.SmartPassed, &s.SelfTestFailed,
		&s.HealthStatus, &s.SmartctlError)
	if err != nil {
		return s, err
	}
	s.Score = scanNullInt(score)
	if s.CapturedAt, err = parseDiskHealthTime(captured); err != nil {
		return s, fmt.Errorf("parse captured_at %q: %w", captured, err)
	}
	return s, nil
}

// GetDiskSamplesSince returns raw samples of a disk captured at or after since, oldest first.
func GetDiskSamplesSince(ctx context.Context, serial string, since time.Time) ([]DiskHealthSample, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT `+diskSampleColumns+` FROM disk_health_samples
		WHERE serial = ? AND captured_at >= ? ORDER BY captured_at ASC, id ASC`, serial, formatDiskHealthTime(since))
	if err != nil {
		return nil, fmt.Errorf("query disk samples %s: %w", serial, err)
	}
	defer rows.Close()
	var out []DiskHealthSample
	for rows.Next() {
		s, err := scanDiskSample(rows)
		if err != nil {
			return nil, fmt.Errorf("scan disk sample %s: %w", serial, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetLatestDiskSamples returns the newest raw sample of every disk that has one.
func GetLatestDiskSamples(ctx context.Context) (map[string]DiskHealthSample, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT `+diskSampleColumns+` FROM disk_health_samples s
		WHERE s.id = (SELECT s2.id FROM disk_health_samples s2 WHERE s2.serial = s.serial ORDER BY s2.captured_at DESC, s2.id DESC LIMIT 1)`)
	if err != nil {
		return nil, fmt.Errorf("query latest disk samples: %w", err)
	}
	defer rows.Close()
	out := map[string]DiskHealthSample{}
	for rows.Next() {
		s, err := scanDiskSample(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest disk sample: %w", err)
		}
		out[s.Serial] = s
	}
	return out, rows.Err()
}

// GetDiskDailySince returns compacted daily rows of a disk on or after the UTC
// day of since, oldest first.
func GetDiskDailySince(ctx context.Context, serial string, since time.Time) ([]DiskHealthDaily, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT serial, day, score, max_temperature_c, power_on_hours, pending_sectors,
			offline_uncorrectable, reallocated_sectors, reported_uncorrectable, media_errors, crc_error_count,
			percentage_used, smart_passed
		FROM disk_health_daily WHERE serial = ? AND day >= ? ORDER BY day ASC`, serial, since.UTC().Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query disk daily %s: %w", serial, err)
	}
	defer rows.Close()
	var out []DiskHealthDaily
	for rows.Next() {
		var d DiskHealthDaily
		var score sql.NullInt64
		if err := rows.Scan(&d.Serial, &d.Day, &score, &d.MaxTemperatureC, &d.PowerOnHours, &d.PendingSectors,
			&d.OfflineUncorrectable, &d.ReallocatedSectors, &d.ReportedUncorrectable, &d.MediaErrors, &d.CRCErrorCount,
			&d.PercentageUsed, &d.SmartPassed); err != nil {
			return nil, fmt.Errorf("scan disk daily %s: %w", serial, err)
		}
		d.Score = scanNullInt(score)
		out = append(out, d)
	}
	return out, rows.Err()
}

const diskColumns = `serial, model, machine_name, device, transport, rotational, capacity_bytes, first_seen, last_seen,
	retired_at, smart_available, score, band, reason, wear_out_at, alert_band, alert_counters`

func scanDisk(row interface{ Scan(...any) error }) (DiskHealthDisk, error) {
	var d DiskHealthDisk
	var firstSeen, lastSeen string
	var retired, wearOut sql.NullString
	var score sql.NullInt64
	err := row.Scan(&d.Serial, &d.Model, &d.MachineName, &d.Device, &d.Transport, &d.Rotational, &d.CapacityBytes,
		&firstSeen, &lastSeen, &retired, &d.SmartAvailable, &score, &d.Band, &d.Reason, &wearOut, &d.AlertBand, &d.AlertCounters)
	if err != nil {
		return d, err
	}
	d.Score = scanNullInt(score)
	if d.FirstSeen, err = parseDiskHealthTime(firstSeen); err != nil {
		return d, fmt.Errorf("parse first_seen: %w", err)
	}
	if d.LastSeen, err = parseDiskHealthTime(lastSeen); err != nil {
		return d, fmt.Errorf("parse last_seen: %w", err)
	}
	if d.RetiredAt, err = scanNullTime(retired); err != nil {
		return d, fmt.Errorf("parse retired_at: %w", err)
	}
	if d.WearOutAt, err = scanNullTime(wearOut); err != nil {
		return d, fmt.Errorf("parse wear_out_at: %w", err)
	}
	return d, nil
}

// ListDisks returns disks by state: "active", "retired" or "all" (default).
func ListDisks(ctx context.Context, state string) ([]DiskHealthDisk, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return nil, err
	}
	where := ""
	switch state {
	case "active":
		where = " WHERE retired_at IS NULL"
	case "retired":
		where = " WHERE retired_at IS NOT NULL"
	case "", "all":
	default:
		return nil, fmt.Errorf("unknown disk state %q", state)
	}
	rows, err := conn.QueryContext(ctx, `SELECT `+diskColumns+` FROM disk_health_disks`+where+` ORDER BY machine_name, device, serial`)
	if err != nil {
		return nil, fmt.Errorf("list disks: %w", err)
	}
	defer rows.Close()
	var out []DiskHealthDisk
	for rows.Next() {
		d, err := scanDisk(rows)
		if err != nil {
			return nil, fmt.Errorf("scan disk: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDisk returns one disk or ErrDiskNotFound.
func GetDisk(ctx context.Context, serial string) (DiskHealthDisk, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return DiskHealthDisk{}, err
	}
	d, err := scanDisk(conn.QueryRowContext(ctx, `SELECT `+diskColumns+` FROM disk_health_disks WHERE serial = ?`, serial))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrDiskNotFound
	}
	if err != nil {
		return d, fmt.Errorf("get disk %s: %w", serial, err)
	}
	return d, nil
}

// LastDiskHealthPoll is the newest last_seen over all disks, or nil when empty.
func LastDiskHealthPoll(ctx context.Context) (*time.Time, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return nil, err
	}
	var last sql.NullString
	if err := conn.QueryRowContext(ctx, `SELECT MAX(last_seen) FROM disk_health_disks`).Scan(&last); err != nil {
		return nil, fmt.Errorf("last disk health poll: %w", err)
	}
	return scanNullTime(last)
}

func UpdateDiskScore(ctx context.Context, serial string, u DiskScoreUpdate) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `UPDATE disk_health_disks SET smart_available = ?, score = ?, band = ?, reason = ?, wear_out_at = ?
		WHERE serial = ?`, u.SmartAvailable, nullInt(u.Score), u.Band, u.Reason, nullDiskHealthTime(u.WearOutAt), serial)
	if err != nil {
		return fmt.Errorf("update disk score %s: %w", serial, err)
	}
	return nil
}

// UpdateDiskAlertState stores the band and counters the last alert evaluation saw.
func UpdateDiskAlertState(ctx context.Context, serial, band, countersJSON string) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE disk_health_disks SET alert_band = ?, alert_counters = ? WHERE serial = ?`,
		band, countersJSON, serial); err != nil {
		return fmt.Errorf("update disk alert state %s: %w", serial, err)
	}
	return nil
}

// RetireStaleDisks marks active disks not seen since cutoff as retired at now.
func RetireStaleDisks(ctx context.Context, cutoff, now time.Time) (int64, error) {
	conn, err := diskHealthDB()
	if err != nil {
		return 0, err
	}
	res, err := conn.ExecContext(ctx, `UPDATE disk_health_disks SET retired_at = ? WHERE retired_at IS NULL AND last_seen < ?`,
		formatDiskHealthTime(now), formatDiskHealthTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("retire stale disks: %w", err)
	}
	return res.RowsAffected()
}

// CompactDiskHealth rolls raw samples older than diskhealth.RawWindow into
// daily rows, deletes them, and deletes daily rows older than
// diskhealth.DailyWindow. It runs in one transaction and is idempotent.
func CompactDiskHealth(ctx context.Context, now time.Time) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	rawCutoff := formatDiskHealthTime(now.Add(-diskhealth.RawWindow))
	dailyCutoff := now.Add(-diskhealth.DailyWindow).UTC().Format("2006-01-02")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("compact disk health: begin: %w", err)
	}
	defer tx.Rollback()

	// The last-of-day values come from the newest compacted sample of that day.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO disk_health_daily (serial, day, score, max_temperature_c, power_on_hours, pending_sectors,
			offline_uncorrectable, reallocated_sectors, reported_uncorrectable, media_errors, crc_error_count,
			percentage_used, smart_passed)
		SELECT s.serial, substr(s.captured_at, 1, 10) AS d, MIN(s.score), MAX(s.temperature_c),
			(SELECT l.power_on_hours FROM disk_health_samples l WHERE l.serial = s.serial AND substr(l.captured_at, 1, 10) = substr(s.captured_at, 1, 10) AND l.captured_at < ?1 ORDER BY l.captured_at DESC, l.id DESC LIMIT 1),
			MAX(s.pending_sectors), MAX(s.offline_uncorrectable), MAX(s.reallocated_sectors),
			MAX(s.reported_uncorrectable), MAX(s.media_errors), MAX(s.crc_error_count),
			(SELECT l.percentage_used FROM disk_health_samples l WHERE l.serial = s.serial AND substr(l.captured_at, 1, 10) = substr(s.captured_at, 1, 10) AND l.captured_at < ?1 ORDER BY l.captured_at DESC, l.id DESC LIMIT 1),
			(SELECT l.smart_passed FROM disk_health_samples l WHERE l.serial = s.serial AND substr(l.captured_at, 1, 10) = substr(s.captured_at, 1, 10) AND l.captured_at < ?1 ORDER BY l.captured_at DESC, l.id DESC LIMIT 1)
		FROM disk_health_samples s
		WHERE s.captured_at < ?1
		GROUP BY s.serial, d
		ON CONFLICT(serial, day) DO UPDATE SET
			score = CASE
				WHEN excluded.score IS NULL THEN disk_health_daily.score
				WHEN disk_health_daily.score IS NULL THEN excluded.score
				ELSE MIN(disk_health_daily.score, excluded.score) END,
			max_temperature_c = MAX(disk_health_daily.max_temperature_c, excluded.max_temperature_c),
			power_on_hours = excluded.power_on_hours,
			pending_sectors = MAX(disk_health_daily.pending_sectors, excluded.pending_sectors),
			offline_uncorrectable = MAX(disk_health_daily.offline_uncorrectable, excluded.offline_uncorrectable),
			reallocated_sectors = MAX(disk_health_daily.reallocated_sectors, excluded.reallocated_sectors),
			reported_uncorrectable = MAX(disk_health_daily.reported_uncorrectable, excluded.reported_uncorrectable),
			media_errors = MAX(disk_health_daily.media_errors, excluded.media_errors),
			crc_error_count = MAX(disk_health_daily.crc_error_count, excluded.crc_error_count),
			percentage_used = excluded.percentage_used,
			smart_passed = excluded.smart_passed`, rawCutoff); err != nil {
		return fmt.Errorf("compact disk health: roll up samples: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM disk_health_samples WHERE captured_at < ?`, rawCutoff); err != nil {
		return fmt.Errorf("compact disk health: delete samples: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM disk_health_daily WHERE day < ?`, dailyCutoff); err != nil {
		return fmt.Errorf("compact disk health: delete daily rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("compact disk health: commit: %w", err)
	}
	return nil
}

// PurgeRetiredDisk deletes a retired disk and all its history. It fails closed
// with ErrDiskNotRetired for an active disk and ErrDiskNotFound for an unknown one.
func PurgeRetiredDisk(ctx context.Context, serial string) error {
	conn, err := diskHealthDB()
	if err != nil {
		return err
	}
	if strings.TrimSpace(serial) == "" {
		return ErrDiskNotFound
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("purge disk %s: begin: %w", serial, err)
	}
	defer tx.Rollback()

	var retired sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT retired_at FROM disk_health_disks WHERE serial = ?`, serial).Scan(&retired)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDiskNotFound
	}
	if err != nil {
		return fmt.Errorf("purge disk %s: %w", serial, err)
	}
	if !retired.Valid {
		return ErrDiskNotRetired
	}
	for _, stmt := range []string{
		`DELETE FROM disk_health_samples WHERE serial = ?`,
		`DELETE FROM disk_health_daily WHERE serial = ?`,
		`DELETE FROM disk_health_disks WHERE serial = ? AND retired_at IS NOT NULL`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, serial); err != nil {
			return fmt.Errorf("purge disk %s: %w", serial, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("purge disk %s: commit: %w", serial, err)
	}
	return nil
}
