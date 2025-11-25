package db

import "time"

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

func DbCreatePushSubscriptionsTable() error {
	const q = `
		CREATE TABLE IF NOT EXISTS push_subscriptions (
			endpoint TEXT PRIMARY KEY,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL
		);
	`
	_, err := DB.Exec(q)
	return err
}

func DbSaveSubscription(sub PushSubscription) error {
	const q = `
		INSERT INTO push_subscriptions (endpoint, p256dh, auth)
		VALUES ($1, $2, $3)
		ON CONFLICT (endpoint) DO UPDATE
		SET p256dh = EXCLUDED.p256dh,
			auth   = EXCLUDED.auth;
	`

	_, err := DB.Exec(q, sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth)
	return err
}

func DbGetAllSubscriptions() ([]PushSubscription, error) {
	rows, err := DB.Query(`SELECT endpoint, p256dh, auth FROM push_subscriptions`)
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

// DbCreateNotsTable creates the `nots` table if it does not exist.
func DbCreateNotsTable() error {
	const q = `
CREATE TABLE IF NOT EXISTS nots (
	id SERIAL PRIMARY KEY,
	title TEXT,
	body TEXT,
	relurl TEXT,
	critical BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err := DB.Exec(q)
	return err
}

// DbSaveNot inserts a notification and then deletes notifications older than 3 months.
func DbSaveNot(n Not) error {
	tx, err := DB.Begin()
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
	VALUES ($1, $2, $3, $4, COALESCE($5, NOW()))
	`
	_, err = tx.Exec(insertQ, n.Title, n.Body, n.RelURL, n.Critical, n.CreatedAt)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	// Delete entries older than 3 months
	cutoff := time.Now().AddDate(0, -snapshotRetentionMonths, 0)
	const delQ = `DELETE FROM nots WHERE created_at < $1`
	if _, err = tx.Exec(delQ, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// DbGetRecentNots returns notifications ordered by newest first.
func DbGetRecentNots(limit int) ([]Not, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT title, body, relurl, critical, created_at FROM nots ORDER BY created_at DESC LIMIT $1`
	rows, err := DB.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Not
	for rows.Next() {
		var n Not
		if err := rows.Scan(&n.Title, &n.Body, &n.RelURL, &n.Critical, &n.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// DbGetNotsFrom returns notifications created at or after `since`, ordered newest first.
func DbGetNotsFrom(since time.Time) ([]Not, error) {
	q := `SELECT title, body, relurl, critical, created_at FROM nots WHERE created_at >= $1 ORDER BY created_at DESC`
	rows, err := DB.Query(q, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Not
	for rows.Next() {
		var n Not
		if err := rows.Scan(&n.Title, &n.Body, &n.RelURL, &n.Critical, &n.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
