package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DailyStreamMetric stores aggregated per-day stats about stream logs.
type DailyStreamMetric struct {
	Day               string             `json:"day"`
	UniqueVisitors    int                `json:"unique_visitors"`
	TotalConnections  int                `json:"total_connections"`
	BytesSent         int64              `json:"bytes_sent"`
	BytesReceived     int64              `json:"bytes_received"`
	AvgSessionSeconds float64            `json:"avg_session_seconds"`
	CountryBreakdown  []CountryBreakdown `json:"country_breakdown"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

// CountryBreakdown describes how many visitors came from a region.
type CountryBreakdown struct {
	Country    string  `json:"country"`
	ISOCode    string  `json:"iso_code"`
	Visitors   int     `json:"visitors"`
	Percentage float64 `json:"percentage"`
}

// CreateStreamDailyMetricsTable ensures the metrics table exists.
func CreateStreamDailyMetricsTable(ctx context.Context) error {
	const query = `
	CREATE TABLE IF NOT EXISTS stream_daily_metrics (
		day TEXT PRIMARY KEY,
		unique_visitors INTEGER NOT NULL,
		total_connections INTEGER NOT NULL,
		bytes_sent INTEGER NOT NULL,
		bytes_received INTEGER NOT NULL,
		avg_session_seconds REAL NOT NULL,
		country_breakdown TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`
	_, err := DB.ExecContext(ctx, query)
	return err
}

// UpsertStreamDailyMetrics stores the provided metrics, replacing existing rows.
func UpsertStreamDailyMetrics(ctx context.Context, metrics []DailyStreamMetric) error {
	if len(metrics) == 0 {
		return nil
	}

	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO stream_daily_metrics (
		day, unique_visitors, total_connections, bytes_sent, bytes_received,
		avg_session_seconds, country_breakdown, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(day) DO UPDATE SET
		unique_visitors=excluded.unique_visitors,
		total_connections=excluded.total_connections,
		bytes_sent=excluded.bytes_sent,
		bytes_received=excluded.bytes_received,
		avg_session_seconds=excluded.avg_session_seconds,
		country_breakdown=excluded.country_breakdown,
		updated_at=excluded.updated_at;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, metric := range metrics {
		payload, marshalErr := marshalCountryBreakdown(metric.CountryBreakdown)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := stmt.ExecContext(
			ctx,
			metric.Day,
			metric.UniqueVisitors,
			metric.TotalConnections,
			metric.BytesSent,
			metric.BytesReceived,
			metric.AvgSessionSeconds,
			payload,
			now.Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetStreamDailyMetrics fetches the most recent per-day stats, ordered descending by day.
func GetStreamDailyMetrics(ctx context.Context, limit int) ([]DailyStreamMetric, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := DB.QueryContext(ctx, `
		SELECT day, unique_visitors, total_connections,
			bytes_sent, bytes_received, avg_session_seconds,
			country_breakdown, updated_at
		FROM stream_daily_metrics
		ORDER BY day DESC
		LIMIT ?;
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []DailyStreamMetric
	for rows.Next() {
		var (
			day            string
			uniqueVisitors int
			totalConn      int
			bytesSent      int64
			bytesRecv      int64
			avgSession     float64
			breakdownJSON  string
			updatedRaw     string
		)
		if err := rows.Scan(
			&day,
			&uniqueVisitors,
			&totalConn,
			&bytesSent,
			&bytesRecv,
			&avgSession,
			&breakdownJSON,
			&updatedRaw,
		); err != nil {
			return nil, err
		}
		breakdown, err := unmarshalCountryBreakdown(breakdownJSON)
		if err != nil {
			return nil, err
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedRaw)
		if err != nil {
			updatedAt = time.Now().UTC()
		}
		metrics = append(metrics, DailyStreamMetric{
			Day:               day,
			UniqueVisitors:    uniqueVisitors,
			TotalConnections:  totalConn,
			BytesSent:         bytesSent,
			BytesReceived:     bytesRecv,
			AvgSessionSeconds: avgSession,
			CountryBreakdown:  breakdown,
			UpdatedAt:         updatedAt,
		})
	}
	return metrics, rows.Err()
}

func marshalCountryBreakdown(items []CountryBreakdown) (string, error) {
	if items == nil {
		items = []CountryBreakdown{}
	}
	bytes, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshal country breakdown: %w", err)
	}
	return string(bytes), nil
}

func unmarshalCountryBreakdown(raw string) ([]CountryBreakdown, error) {
	if strings.TrimSpace(raw) == "" {
		return []CountryBreakdown{}, nil
	}
	var items []CountryBreakdown
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("unmarshal country breakdown: %w", err)
	}
	return items, nil
}
