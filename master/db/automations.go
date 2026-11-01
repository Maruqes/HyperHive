package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AutomationRun is one HH Automation run. Output is only loaded by GetAutomationRun.
type AutomationRun struct {
	ID              string         `json:"id"`
	AutomationID    string         `json:"automation_id"`
	AutomationName  string         `json:"automation_name"`
	Trigger         string         `json:"trigger"`
	Condition       string         `json:"condition"`
	Snapshot        map[string]any `json:"snapshot"`
	Status          string         `json:"status"`
	ExitCode        *int           `json:"exit_code"`
	StartedAt       time.Time      `json:"started_at"`
	EndedAt         *time.Time     `json:"ended_at"`
	OutputTruncated bool           `json:"output_truncated"`
	Error           string         `json:"error"`
	Output          *string        `json:"output,omitempty"`
}

// AutomationSettings holds master-side run retention. RetentionCount is per automation.
type AutomationSettings struct {
	RetentionDays  int `json:"retention_days"`
	RetentionCount int `json:"retention_count"`
}

var DefaultAutomationSettings = AutomationSettings{RetentionDays: 30, RetentionCount: 200}

func CreateAutomationTables(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS automation_runs (
		id TEXT PRIMARY KEY,
		automation_id TEXT NOT NULL,
		automation_name TEXT NOT NULL DEFAULT '',
		trigger_type TEXT NOT NULL,
		condition TEXT NOT NULL DEFAULT '',
		snapshot TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL,
		exit_code INTEGER,
		started_at INTEGER NOT NULL,
		ended_at INTEGER,
		output TEXT NOT NULL DEFAULT '',
		output_truncated INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '',
		pgid INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS automation_runs_by_automation ON automation_runs(automation_id, started_at DESC);
	CREATE TABLE IF NOT EXISTS automation_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		retention_days INTEGER NOT NULL,
		retention_count INTEGER NOT NULL
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

const automationRunColumns = `id, automation_id, automation_name, trigger_type, condition, snapshot, status, exit_code, started_at, ended_at, output_truncated, error`

type rowScanner interface{ Scan(...any) error }

func scanAutomationRun(row rowScanner, extra ...any) (AutomationRun, error) {
	var run AutomationRun
	var snapshot string
	var exitCode sql.NullInt64
	var started int64
	var ended sql.NullInt64
	targets := append([]any{&run.ID, &run.AutomationID, &run.AutomationName, &run.Trigger, &run.Condition, &snapshot, &run.Status, &exitCode, &started, &ended, &run.OutputTruncated, &run.Error}, extra...)
	if err := row.Scan(targets...); err != nil {
		return run, err
	}
	if err := json.Unmarshal([]byte(snapshot), &run.Snapshot); err != nil || run.Snapshot == nil {
		run.Snapshot = map[string]any{}
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		run.ExitCode = &code
	}
	run.StartedAt = time.UnixMilli(started).UTC()
	if ended.Valid {
		at := time.UnixMilli(ended.Int64).UTC()
		run.EndedAt = &at
	}
	return run, nil
}

func InsertAutomationRun(ctx context.Context, run AutomationRun) error {
	snapshot, err := json.Marshal(run.Snapshot)
	if err != nil || run.Snapshot == nil {
		snapshot = []byte("{}")
	}
	_, err = DB.ExecContext(ctx, `INSERT INTO automation_runs (id, automation_id, automation_name, trigger_type, condition, snapshot, status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
		run.ID, run.AutomationID, run.AutomationName, run.Trigger, run.Condition, string(snapshot), run.Status, run.StartedAt.UnixMilli())
	return err
}

func SetAutomationRunPGID(ctx context.Context, id string, pgid int) error {
	_, err := DB.ExecContext(ctx, `UPDATE automation_runs SET pgid = ? WHERE id = ?;`, pgid, id)
	return err
}

func FinishAutomationRun(ctx context.Context, id, status string, exitCode *int, endedAt time.Time, output string, truncated bool, errText string) error {
	_, err := DB.ExecContext(ctx, `UPDATE automation_runs SET status = ?, exit_code = ?, ended_at = ?, output = ?, output_truncated = ?, error = ?, pgid = 0 WHERE id = ?;`,
		status, exitCode, endedAt.UnixMilli(), output, truncated, errText, id)
	return err
}

// GetAutomationRun returns one run with its output, or sql.ErrNoRows.
func GetAutomationRun(ctx context.Context, id string) (AutomationRun, error) {
	var output string
	row := DB.QueryRowContext(ctx, `SELECT `+automationRunColumns+`, output FROM automation_runs WHERE id = ?;`, id)
	run, err := scanAutomationRun(row, &output)
	if err != nil {
		return AutomationRun{}, err
	}
	run.Output = &output
	return run, nil
}

func queryAutomationRuns(ctx context.Context, query string, args ...any) ([]AutomationRun, error) {
	rows, err := DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []AutomationRun{}
	for rows.Next() {
		run, err := scanAutomationRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// ListAutomationRuns returns runs newest first; an empty automationID lists all.
func ListAutomationRuns(ctx context.Context, automationID string, limit int) ([]AutomationRun, error) {
	if automationID == "" {
		return queryAutomationRuns(ctx, `SELECT `+automationRunColumns+` FROM automation_runs ORDER BY started_at DESC, id LIMIT ?;`, limit)
	}
	return queryAutomationRuns(ctx, `SELECT `+automationRunColumns+` FROM automation_runs WHERE automation_id = ? ORDER BY started_at DESC, id LIMIT ?;`, automationID, limit)
}

// LatestAutomationRuns returns the newest run of every automation, by automation ID.
func LatestAutomationRuns(ctx context.Context) (map[string]AutomationRun, error) {
	runs, err := queryAutomationRuns(ctx, `SELECT `+automationRunColumns+` FROM automation_runs r
		WHERE id = (SELECT id FROM automation_runs WHERE automation_id = r.automation_id ORDER BY started_at DESC, id LIMIT 1);`)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]AutomationRun, len(runs))
	for _, run := range runs {
		latest[run.AutomationID] = run
	}
	return latest, nil
}

// RunningAutomationRuns returns runs still marked running (used at startup).
func RunningAutomationRuns(ctx context.Context) ([]AutomationRun, error) {
	return queryAutomationRuns(ctx, `SELECT `+automationRunColumns+` FROM automation_runs WHERE status = 'running';`)
}

func DeleteAutomationRuns(ctx context.Context, automationID string) error {
	_, err := DB.ExecContext(ctx, `DELETE FROM automation_runs WHERE automation_id = ?;`, automationID)
	return err
}

// PruneAutomationRuns removes finished runs older than days and beyond the
// newest count per automation.
func PruneAutomationRuns(ctx context.Context, settings AutomationSettings, now time.Time) error {
	cutoff := now.AddDate(0, 0, -settings.RetentionDays).UnixMilli()
	if _, err := DB.ExecContext(ctx, `DELETE FROM automation_runs WHERE status != 'running' AND started_at < ?;`, cutoff); err != nil {
		return err
	}
	_, err := DB.ExecContext(ctx, `DELETE FROM automation_runs WHERE status != 'running' AND id IN (
		SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY automation_id ORDER BY started_at DESC, id) AS position FROM automation_runs)
		WHERE position > ?);`, settings.RetentionCount)
	return err
}

func GetAutomationSettings(ctx context.Context) (AutomationSettings, error) {
	var settings AutomationSettings
	err := DB.QueryRowContext(ctx, `SELECT retention_days, retention_count FROM automation_settings WHERE id = 1;`).Scan(&settings.RetentionDays, &settings.RetentionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultAutomationSettings, nil
	}
	return settings, err
}

func SaveAutomationSettings(ctx context.Context, settings AutomationSettings) error {
	_, err := DB.ExecContext(ctx, `INSERT INTO automation_settings (id, retention_days, retention_count) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET retention_days = excluded.retention_days, retention_count = excluded.retention_count;`,
		settings.RetentionDays, settings.RetentionCount)
	return err
}
