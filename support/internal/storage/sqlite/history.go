package sqlite

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"hyperhive/support/internal/domain"
)

func (store *Store) History(app domain.App, query domain.HistoryQuery, now time.Time) (domain.History, error) {
	streamName := query.Stream
	cutoff := now.Add(-time.Duration(app.Retention) * 24 * time.Hour)
	if query.Since != nil {
		since := *query.Since
		if since.After(now) {
			since = now.Add(time.Second)
		}
		if since.After(cutoff) {
			cutoff = since
		}
	}
	limit := query.Limit
	cursorKind := ""
	var cursor int64
	if query.Before != nil {
		cursorKind = "before"
		cursor = *query.Before
	}
	if query.After != nil {
		cursorKind = "after"
		cursor = *query.After
	}
	where := "app_id=? AND stream=? AND received_at>=?"
	args := []any{app.ID, streamName, cutoff.UnixNano()}
	if len(query.Severity) > 0 {
		severities := query.Severity
		placeholders := []string{}
		for _, severity := range severities {
			if !domain.ValidSeverity(severity) {
				return domain.History{}, domain.Invalid("invalid severity")
			}
			placeholders = append(placeholders, "?")
			args = append(args, severity)
		}
		where += " AND json_extract(value,'$.severity') IN (" + strings.Join(placeholders, ",") + ")"
	}
	baseWhere := where
	baseArgs := append([]any(nil), args...)
	order := "DESC"
	if cursorKind == "before" {
		where += " AND id<?"
		args = append(args, cursor)
	}
	if cursorKind == "after" {
		where += " AND id>?"
		args = append(args, cursor)
		order = "ASC"
	}
	args = append(args, limit)
	rows, err := store.db.Query("SELECT id,stream,instance_id,timestamp,received_at,value FROM samples WHERE "+where+" ORDER BY id "+order+" LIMIT ?", args...)
	if err != nil {
		return domain.History{}, err
	}
	samples := []domain.Sample{}
	for rows.Next() {
		var sample domain.Sample
		var instance sql.NullString
		var timestamp, value string
		var received int64
		if err = rows.Scan(&sample.ID, &sample.Stream, &instance, &timestamp, &received, &value); err != nil {
			rows.Close()
			return domain.History{}, err
		}
		sample.AppID = app.ID
		if instance.Valid {
			sample.InstanceID = &instance.String
		}
		sample.Timestamp, err = time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			rows.Close()
			return domain.History{}, err
		}
		sample.ReceivedAt = time.Unix(0, received).UTC()
		sample.Value = json.RawMessage(value)
		samples = append(samples, sample)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return domain.History{}, err
	}
	if order == "DESC" {
		for left, right := 0, len(samples)-1; left < right; left, right = left+1, right-1 {
			samples[left], samples[right] = samples[right], samples[left]
		}
	}
	var nextBefore *int64
	if len(samples) > 0 {
		var exists int
		args = append(baseArgs, samples[0].ID)
		if err = store.db.QueryRow("SELECT EXISTS(SELECT 1 FROM samples WHERE "+baseWhere+" AND id<?)", args...).Scan(&exists); err != nil {
			return domain.History{}, err
		}
		if exists != 0 {
			oldest := samples[0].ID
			nextBefore = &oldest
		}
	}
	return domain.History{Samples: samples, NextBefore: nextBefore}, nil
}
