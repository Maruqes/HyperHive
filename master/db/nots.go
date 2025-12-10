package db

import (
	"context"
	"time"
)

type PushSubscription struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime"`
	Keys           PSKeys `json:"keys"`
}

type PSKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// Not represents a notification to store in the DB.
type Not struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	RelURL    string    `json:"relURL"`
	Critical  bool      `json:"critical"`
	CreatedAt time.Time `json:"createdAt"`
}

func DbCreatePushSubscriptionsTable(ctx context.Context) error {
	const q = `
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			endpoint TEXT PRIMARY KEY,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL
		);
	`
	_, err := DB.ExecContext(ctx, q)
	return err
}

func DbSaveSubscription(ctx context.Context, sub PushSubscription) error {
	const q = `
		INSERT INTO push_subscriptions (endpoint, p256dh, auth)
		VALUES ($1, $2, $3)
		ON CONFLICT (endpoint) DO UPDATE
		SET p256dh = EXCLUDED.p256dh,
			auth   = EXCLUDED.auth;
	`

	_, err := DB.ExecContext(ctx, q, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth)
	return err
}

func DbGetAllSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := DB.QueryContext(ctx, `SELECT endpoint, p256dh, auth FROM push_subscriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.Endpoint, &s.Keys.P256dh, &s.Keys.Auth); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return subs, nil
}

// DbDeleteAllSubscriptions removes all rows from push_subscriptions.
func DbDeleteAllSubscriptions(ctx context.Context) error {
	const q = `DELETE FROM push_subscriptions`
	_, err := DB.ExecContext(ctx, q)
	return err
}

// DbCreateNotsTable creates the `nots` table if it does not exist.
func DbCreateNotsTable(ctx context.Context) error {
	const q = `
CREATE TABLE IF NOT EXISTS nots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT,
	body TEXT,
	relurl TEXT,
	critical BOOLEAN NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
	_, err := DB.ExecContext(ctx, q)
	return err
}

// DbSaveNot inserts a notification and then deletes notifications older than 3 months.
func DbSaveNot(ctx context.Context, n Not) error {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const insertQ = `
	INSERT INTO nots (title, body, relurl, critical, created_at)
	VALUES (?, ?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP))
	`

	var created interface{}
	if n.CreatedAt.IsZero() {
		created = nil
	} else {
		created = formatSnapshotTime(n.CreatedAt)
	}

	_, err = tx.Exec(insertQ, n.Title, n.Body, n.RelURL, n.Critical, created)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	// Delete entries older than 3 months (use datetime() for robust comparison)
	cutoff := formatSnapshotTime(time.Now().AddDate(0, -snapshotRetentionMonths, 0))
	const delQ = `DELETE FROM nots WHERE datetime(created_at) < datetime(?)`
	if _, err = tx.Exec(delQ, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// DbGetRecentNots returns notifications ordered by newest first.
func DbGetRecentNots(ctx context.Context, limit int) ([]Not, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT title, body, relurl, critical, created_at FROM nots ORDER BY datetime(created_at) DESC LIMIT ?`
	rows, err := DB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Not
	for rows.Next() {
		var n Not
		var createdAtStr string
		if err := rows.Scan(&n.Title, &n.Body, &n.RelURL, &n.Critical, &createdAtStr); err != nil {
			return nil, err
		}
		// Try parsing stored timestamp in multiple formats (RFC3339Nano first, then SQLite default)
		if t, err := parseSnapshotTime(createdAtStr); err == nil {
			n.CreatedAt = t
		} else if t2, err2 := time.Parse("2006-01-02 15:04:05", createdAtStr); err2 == nil {
			n.CreatedAt = t2
		} else if t3, err3 := time.Parse(time.RFC3339, createdAtStr); err3 == nil {
			n.CreatedAt = t3
		} else {
			n.CreatedAt = time.Time{}
		}
		res = append(res, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// DbGetNotsFrom returns notifications created at or after `since`, ordered newest first.
func DbGetNotsFrom(ctx context.Context, since time.Time) ([]Not, error) {
	q := `SELECT title, body, relurl, critical, created_at FROM nots WHERE datetime(created_at) >= datetime(?) ORDER BY datetime(created_at) DESC`
	rows, err := DB.QueryContext(ctx, q, formatSnapshotTime(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Not
	for rows.Next() {
		var n Not
		var createdAtStr string
		if err := rows.Scan(&n.Title, &n.Body, &n.RelURL, &n.Critical, &createdAtStr); err != nil {
			return nil, err
		}
		if t, err := parseSnapshotTime(createdAtStr); err == nil {
			n.CreatedAt = t
		} else if t2, err2 := time.Parse("2006-01-02 15:04:05", createdAtStr); err2 == nil {
			n.CreatedAt = t2
		} else if t3, err3 := time.Parse(time.RFC3339, createdAtStr); err3 == nil {
			n.CreatedAt = t3
		} else {
			n.CreatedAt = time.Time{}
		}
		res = append(res, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
