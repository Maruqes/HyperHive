package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// NetworkHistoryDB is apart from data.db so charts stay out of its 14 days of backups.
// It stays nil if opening failed: losing charts must not stop the master.
var NetworkHistoryDB *sql.DB

const (
	networkHistoryDSN = "file:network_history.db?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	// This key marks a bucket as sampled, so idle reads as zero and an outage as a gap.
	networkHistoryCoverageKey = ""
)

// Every sample goes into all tiers, so no rollup job has to merge old rows.
var networkHistoryTiers = []struct {
	Resolution int64
	Retention  time.Duration
}{
	{60, 25 * time.Hour},
	{15 * 60, 31 * 24 * time.Hour},
	{6 * 3600, 366 * 24 * time.Hour},
}

// NetworkHistorySample is what one key moved since the previous sample, peaks in bytes per second.
type NetworkHistorySample struct {
	Sent         uint64
	Received     uint64
	PeakSent     float64
	PeakReceived float64
}

// NetworkHistoryBucket sums the requested keys, so its peaks are an upper bound: they may not coincide.
type NetworkHistoryBucket struct {
	Bucket int64
	NetworkHistorySample
}

func InitNetworkHistoryDB(ctx context.Context) error {
	conn, err := sql.Open("sqlite3", networkHistoryDSN)
	if err != nil {
		return fmt.Errorf("open network history: %w", err)
	}
	if err := createNetworkHistoryTable(ctx, conn); err != nil {
		conn.Close()
		return err
	}
	NetworkHistoryDB = conn
	return nil
}

func createNetworkHistoryTable(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS network_history (
		resolution INTEGER NOT NULL,
		bucket INTEGER NOT NULL,
		key TEXT NOT NULL,
		sent INTEGER NOT NULL,
		received INTEGER NOT NULL,
		peak_sent REAL NOT NULL,
		peak_received REAL NOT NULL,
		PRIMARY KEY (resolution, key, bucket)
	) WITHOUT ROWID`)
	if err != nil {
		return fmt.Errorf("create network history table: %w", err)
	}
	return nil
}

// RecordNetworkHistory stores one sample. Keys that moved nothing are left out by the caller.
func RecordNetworkHistory(ctx context.Context, at time.Time, samples map[string]NetworkHistorySample) error {
	if NetworkHistoryDB == nil {
		return nil
	}
	tx, err := NetworkHistoryDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record network history: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO network_history (resolution, bucket, key, sent, received, peak_sent, peak_received)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (resolution, key, bucket) DO UPDATE SET
		sent = sent + excluded.sent,
		received = received + excluded.received,
		peak_sent = MAX(peak_sent, excluded.peak_sent),
		peak_received = MAX(peak_received, excluded.peak_received)`)
	if err != nil {
		return fmt.Errorf("record network history: %w", err)
	}
	defer stmt.Close()
	for _, tier := range networkHistoryTiers {
		bucket := at.Unix() - at.Unix()%tier.Resolution
		if _, err := stmt.ExecContext(ctx, tier.Resolution, bucket, networkHistoryCoverageKey, 0, 0, 0, 0); err != nil {
			return fmt.Errorf("record network history coverage: %w", err)
		}
		for key, sample := range samples {
			if _, err := stmt.ExecContext(ctx, tier.Resolution, bucket, key, int64(sample.Sent), int64(sample.Received), sample.PeakSent, sample.PeakReceived); err != nil {
				return fmt.Errorf("record network history %s: %w", key, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record network history: %w", err)
	}
	return nil
}

// CleanNetworkHistory drops what each tier no longer keeps.
func CleanNetworkHistory(ctx context.Context, now time.Time) error {
	if NetworkHistoryDB == nil {
		return nil
	}
	for _, tier := range networkHistoryTiers {
		cutoff := now.Add(-tier.Retention).Unix()
		if _, err := NetworkHistoryDB.ExecContext(ctx, `DELETE FROM network_history WHERE resolution = ? AND bucket < ?`, tier.Resolution, cutoff); err != nil {
			return fmt.Errorf("clean network history: %w", err)
		}
	}
	return nil
}

// GetNetworkHistory returns the sampled buckets from `from` on, oldest first. A bucket
// that was never sampled has no row, and a sampled one with no traffic comes back as zero.
func GetNetworkHistory(ctx context.Context, keys []string, resolution, from int64) ([]NetworkHistoryBucket, error) {
	if NetworkHistoryDB == nil {
		return nil, fmt.Errorf("network history is not available")
	}
	args := []any{resolution, from, networkHistoryCoverageKey}
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := NetworkHistoryDB.QueryContext(ctx, `
	SELECT bucket, SUM(sent), SUM(received), SUM(peak_sent), SUM(peak_received)
	FROM network_history
	WHERE resolution = ? AND bucket >= ? AND key IN (?`+strings.Repeat(",?", len(keys))+`)
	GROUP BY bucket ORDER BY bucket`, args...)
	if err != nil {
		return nil, fmt.Errorf("read network history: %w", err)
	}
	defer rows.Close()
	out := []NetworkHistoryBucket{}
	for rows.Next() {
		var bucket NetworkHistoryBucket
		var sent, received int64
		if err := rows.Scan(&bucket.Bucket, &sent, &received, &bucket.PeakSent, &bucket.PeakReceived); err != nil {
			return nil, fmt.Errorf("read network history: %w", err)
		}
		bucket.Sent, bucket.Received = uint64(sent), uint64(received)
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read network history: %w", err)
	}
	return out, nil
}
