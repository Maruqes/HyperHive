package db

type PushSubscription struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime"`
	Keys           PSKeys `json:"keys"`
}

type PSKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
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
