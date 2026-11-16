package db

import (
	"context"
	"fmt"
)

// EnergySampleSteps are the bucket sizes, in seconds, every sample is written to.
var EnergySampleSteps = [...]int64{300, 1800, 3600}

// EnergySample is the energy of one host (Kind "host", Name empty) or VM
// (Kind "vm") inside one time bucket. Ts is the bucket start in Unix seconds.
type EnergySample struct {
	Ts       int64
	Kind     string
	Machine  string
	Name     string
	Wh       float64
	RunningS float64
}

const energySamplesTable = `
	CREATE TABLE IF NOT EXISTS energy_samples (
		step INTEGER NOT NULL,
		ts INTEGER NOT NULL,
		kind TEXT NOT NULL,
		machine TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		wh REAL NOT NULL DEFAULT 0,
		running_s REAL NOT NULL DEFAULT 0,
		PRIMARY KEY (step, kind, machine, name, ts)
	);
`

// AddEnergySamples adds every sample to its bucket at each step, in one
// transaction. Ts is the sample time; the bucket start is derived from it.
func AddEnergySamples(ctx context.Context, samples []EnergySample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO energy_samples (step, ts, kind, machine, name, wh, running_s)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(step, kind, machine, name, ts) DO UPDATE SET
			wh = wh + excluded.wh,
			running_s = running_s + excluded.running_s;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, sample := range samples {
		for _, step := range EnergySampleSteps {
			if _, err := stmt.ExecContext(ctx, step, sample.Ts-sample.Ts%step, sample.Kind, sample.Machine, sample.Name, sample.Wh, sample.RunningS); err != nil {
				return fmt.Errorf("add energy sample %s/%s/%s: %w", sample.Kind, sample.Machine, sample.Name, err)
			}
		}
	}
	return tx.Commit()
}

// GetEnergySamples returns the buckets of one step from ts `from` on. Empty
// kind, machine or name match everything.
func GetEnergySamples(ctx context.Context, step, from int64, kind, machine, name string) ([]EnergySample, error) {
	const query = `
	SELECT ts, kind, machine, name, wh, running_s
	FROM energy_samples
	WHERE step = ? AND ts >= ?
		AND (? = '' OR kind = ?) AND (? = '' OR machine = ?) AND (? = '' OR name = ?)
	ORDER BY ts;
	`
	rows, err := DB.QueryContext(ctx, query, step, from, kind, kind, machine, machine, name, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EnergySample{}
	for rows.Next() {
		var s EnergySample
		if err := rows.Scan(&s.Ts, &s.Kind, &s.Machine, &s.Name, &s.Wh, &s.RunningS); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetEnergySampleTotals returns one row per series: the summed Wh and running
// seconds of its buckets from ts `from` on, with Ts set to its first bucket.
func GetEnergySampleTotals(ctx context.Context, step, from int64) ([]EnergySample, error) {
	const query = `
	SELECT MIN(ts), kind, machine, name, SUM(wh), SUM(running_s)
	FROM energy_samples
	WHERE step = ? AND ts >= ?
	GROUP BY kind, machine, name;
	`
	rows, err := DB.QueryContext(ctx, query, step, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EnergySample{}
	for rows.Next() {
		var s EnergySample
		if err := rows.Scan(&s.Ts, &s.Kind, &s.Machine, &s.Name, &s.Wh, &s.RunningS); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteEnergySamplesBefore drops the buckets of one step older than ts.
func DeleteEnergySamplesBefore(ctx context.Context, step, ts int64) error {
	_, err := DB.ExecContext(ctx, `DELETE FROM energy_samples WHERE step = ? AND ts < ?;`, step, ts)
	return err
}
