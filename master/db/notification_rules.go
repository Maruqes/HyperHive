package db

import (
	"context"
	"database/sql"
	"errors"
)

// NotificationRule is the stored delivery policy for one notification category.
// Quiet hours use "HH:MM" in the master's local time; both empty means none.
type NotificationRule struct {
	Category        string `json:"category"`
	Enabled         bool   `json:"enabled"`
	MinPushSeverity string `json:"minPushSeverity"`
	QuietStart      string `json:"quietStart"`
	QuietEnd        string `json:"quietEnd"`
}

func DbCreateNotificationRulesTable(ctx context.Context) error {
	const q = `
CREATE TABLE IF NOT EXISTS notification_rules (
	category TEXT PRIMARY KEY,
	enabled BOOLEAN NOT NULL DEFAULT 1,
	min_push_severity TEXT NOT NULL,
	quiet_start TEXT NOT NULL DEFAULT '',
	quiet_end TEXT NOT NULL DEFAULT ''
);
`
	_, err := DB.ExecContext(ctx, q)
	return err
}

// DbGetNotificationRule returns the stored rule; ok is false when the category uses defaults.
func DbGetNotificationRule(ctx context.Context, category string) (rule NotificationRule, ok bool, err error) {
	err = DB.QueryRowContext(ctx, `SELECT category, enabled, min_push_severity, quiet_start, quiet_end FROM notification_rules WHERE category = ?`, category).
		Scan(&rule.Category, &rule.Enabled, &rule.MinPushSeverity, &rule.QuietStart, &rule.QuietEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return NotificationRule{}, false, nil
	}
	if err != nil {
		return NotificationRule{}, false, err
	}
	return rule, true, nil
}

func DbSaveNotificationRule(ctx context.Context, rule NotificationRule) error {
	const q = `
INSERT INTO notification_rules (category, enabled, min_push_severity, quiet_start, quiet_end)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (category) DO UPDATE
SET enabled = EXCLUDED.enabled,
	min_push_severity = EXCLUDED.min_push_severity,
	quiet_start = EXCLUDED.quiet_start,
	quiet_end = EXCLUDED.quiet_end;
`
	_, err := DB.ExecContext(ctx, q, rule.Category, rule.Enabled, rule.MinPushSeverity, rule.QuietStart, rule.QuietEnd)
	return err
}
