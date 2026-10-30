package db

import (
	"context"
	"time"
)

// FirebaseDevice is an Android app installation that receives FCM notifications.
type FirebaseDevice struct {
	Token     string    `json:"-"`
	ProjectID string    `json:"projectId"`
	UserID    int64     `json:"userId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func DbCreateFirebaseDevicesTable(ctx context.Context) error {
	const q = `
CREATE TABLE IF NOT EXISTS firebase_devices (
	token TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	user_id INTEGER NOT NULL DEFAULT 0,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
	_, err := DB.ExecContext(ctx, q)
	return err
}

// DbSaveFirebaseDevice registers a token or refreshes its owner and project.
func DbSaveFirebaseDevice(ctx context.Context, d FirebaseDevice) error {
	const q = `
INSERT INTO firebase_devices (token, project_id, user_id, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (token) DO UPDATE
SET project_id = EXCLUDED.project_id,
	user_id = EXCLUDED.user_id,
	updated_at = EXCLUDED.updated_at;
`
	_, err := DB.ExecContext(ctx, q, d.Token, d.ProjectID, d.UserID, formatSnapshotTime(time.Now()))
	return err
}

func DbDeleteFirebaseDevice(ctx context.Context, token string) error {
	_, err := DB.ExecContext(ctx, `DELETE FROM firebase_devices WHERE token = ?`, token)
	return err
}

// DbGetFirebaseDevices returns the devices registered for one Firebase project.
func DbGetFirebaseDevices(ctx context.Context, projectID string) ([]FirebaseDevice, error) {
	rows, err := DB.QueryContext(ctx, `SELECT token, project_id, user_id FROM firebase_devices WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []FirebaseDevice
	for rows.Next() {
		var d FirebaseDevice
		if err := rows.Scan(&d.Token, &d.ProjectID, &d.UserID); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}
