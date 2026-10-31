package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PowerSchedule is the single cluster-wide power schedule. Start and Stop are
// "HH:MM" in TZ (the master's IANA zone when saved).
type PowerSchedule struct {
	Enabled   bool      `json:"enabled"`
	Start     string    `json:"start"`
	Stop      string    `json:"stop"`
	TZ        string    `json:"tz"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PowerScheduleStatus is the last apply attempt on one machine. Stop, Wake, JobID
// and AppliedAt come from the last successful apply; LastError and AttemptedAt
// from the latest attempt (LastError is empty when it succeeded).
type PowerScheduleStatus struct {
	Machine     string    `json:"machine"`
	Enabled     bool      `json:"enabled"`
	StopUnix    int64     `json:"stopUnix"`
	WakeUnix    int64     `json:"wakeUnix"`
	JobID       string    `json:"jobId"`
	AppliedAt   time.Time `json:"appliedAt"`
	LastError   string    `json:"lastError"`
	AttemptedAt time.Time `json:"attemptedAt"`
}

// DefaultPowerSchedule is returned before the schedule is ever saved.
var DefaultPowerSchedule = PowerSchedule{Enabled: false, Start: "08:00", Stop: "23:00"}

func CreatePowerScheduleTables(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS power_schedule (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled INTEGER NOT NULL DEFAULT 0,
		start_hhmm TEXT NOT NULL,
		stop_hhmm TEXT NOT NULL,
		tz TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS power_schedule_status (
		machine TEXT PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 0,
		stop_unix INTEGER NOT NULL DEFAULT 0,
		wake_unix INTEGER NOT NULL DEFAULT 0,
		job_id TEXT NOT NULL DEFAULT '',
		applied_at INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		attempted_at INTEGER NOT NULL DEFAULT 0
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

// GetPowerSchedule returns the saved schedule, or DefaultPowerSchedule if none was saved.
func GetPowerSchedule(ctx context.Context) (PowerSchedule, error) {
	const query = `SELECT enabled, start_hhmm, stop_hhmm, tz, updated_at FROM power_schedule WHERE id = 1;`
	var s PowerSchedule
	var updated int64
	err := DB.QueryRowContext(ctx, query).Scan(&s.Enabled, &s.Start, &s.Stop, &s.TZ, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPowerSchedule, nil
	}
	if err != nil {
		return PowerSchedule{}, err
	}
	s.UpdatedAt = unixOrZero(updated)
	return s, nil
}

func SavePowerSchedule(ctx context.Context, s PowerSchedule) error {
	const query = `
	INSERT INTO power_schedule (id, enabled, start_hhmm, stop_hhmm, tz, updated_at)
	VALUES (1, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		enabled = excluded.enabled,
		start_hhmm = excluded.start_hhmm,
		stop_hhmm = excluded.stop_hhmm,
		tz = excluded.tz,
		updated_at = excluded.updated_at;
	`
	_, err := DB.ExecContext(ctx, query, s.Enabled, s.Start, s.Stop, s.TZ, s.UpdatedAt.Unix())
	return err
}

// RecordPowerScheduleApplied stores a successful apply and clears the last error.
func RecordPowerScheduleApplied(ctx context.Context, st PowerScheduleStatus) error {
	const query = `
	INSERT INTO power_schedule_status (machine, enabled, stop_unix, wake_unix, job_id, applied_at, last_error, attempted_at)
	VALUES (?, ?, ?, ?, ?, ?, '', ?)
	ON CONFLICT(machine) DO UPDATE SET
		enabled = excluded.enabled,
		stop_unix = excluded.stop_unix,
		wake_unix = excluded.wake_unix,
		job_id = excluded.job_id,
		applied_at = excluded.applied_at,
		last_error = '',
		attempted_at = excluded.attempted_at;
	`
	_, err := DB.ExecContext(ctx, query, st.Machine, st.Enabled, st.StopUnix, st.WakeUnix, st.JobID, st.AppliedAt.Unix(), st.AppliedAt.Unix())
	return err
}

// RecordPowerScheduleFailure stores a failed attempt and keeps the last successful values.
func RecordPowerScheduleFailure(ctx context.Context, machine, lastError string, attemptedAt time.Time) error {
	const query = `
	INSERT INTO power_schedule_status (machine, last_error, attempted_at)
	VALUES (?, ?, ?)
	ON CONFLICT(machine) DO UPDATE SET
		last_error = excluded.last_error,
		attempted_at = excluded.attempted_at;
	`
	_, err := DB.ExecContext(ctx, query, machine, lastError, attemptedAt.Unix())
	return err
}

func GetPowerScheduleStatuses(ctx context.Context) ([]PowerScheduleStatus, error) {
	const query = `
	SELECT machine, enabled, stop_unix, wake_unix, job_id, applied_at, last_error, attempted_at
	FROM power_schedule_status
	ORDER BY machine;
	`
	rows, err := DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := []PowerScheduleStatus{}
	for rows.Next() {
		var st PowerScheduleStatus
		var applied, attempted int64
		if err := rows.Scan(&st.Machine, &st.Enabled, &st.StopUnix, &st.WakeUnix, &st.JobID, &applied, &st.LastError, &attempted); err != nil {
			return nil, err
		}
		st.AppliedAt = unixOrZero(applied)
		st.AttemptedAt = unixOrZero(attempted)
		statuses = append(statuses, st)
	}
	return statuses, rows.Err()
}

func unixOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
