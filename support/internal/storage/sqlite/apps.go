package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"time"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/security"
)

func (store *Store) App(id string, now time.Time) (domain.App, error) {
	var app domain.App
	var config string
	var directJSON sql.NullString
	if err := store.db.QueryRow("SELECT config,direct FROM apps WHERE id=?", id).Scan(&config, &directJSON); err != nil {
		if err == sql.ErrNoRows {
			return app, domain.NotFound()
		}
		return app, err
	}
	if err := json.Unmarshal([]byte(config), &app); err != nil {
		return app, err
	}
	app.Instances = []domain.Instance{}
	rows, err := store.db.Query("SELECT state FROM instances WHERE app_id=? ORDER BY id", id)
	if err != nil {
		return app, err
	}
	for rows.Next() {
		var state string
		if err = rows.Scan(&state); err != nil {
			rows.Close()
			return app, err
		}
		var instance domain.Instance
		if err = json.Unmarshal([]byte(state), &instance); err != nil {
			rows.Close()
			return app, err
		}
		app.Instances = append(app.Instances, instance)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return app, err
	}
	var direct *domain.Instance
	if directJSON.Valid {
		direct = &domain.Instance{}
		if err = json.Unmarshal([]byte(directJSON.String), direct); err != nil {
			return app, err
		}
	}
	domain.Aggregate(&app, direct, now)
	return app, nil
}
func (store *Store) Apps(now time.Time) ([]domain.App, error) {
	rows, err := store.db.Query("SELECT id FROM apps ORDER BY id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	apps := []domain.App{}
	for _, id := range ids {
		app, err := store.App(id, now)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, nil
}
func (store *Store) Streams(id string) ([]domain.Stream, error) {
	rows, err := store.db.Query("SELECT definition FROM streams WHERE app_id=? ORDER BY name", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	streams := []domain.Stream{}
	for rows.Next() {
		var definition string
		if err = rows.Scan(&definition); err != nil {
			return nil, err
		}
		var stream domain.Stream
		if err = json.Unmarshal([]byte(definition), &stream); err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	return streams, rows.Err()
}
func (store *Store) Layout(id string) (domain.Layout, error) {
	var layout domain.Layout
	var value string
	if err := store.db.QueryRow("SELECT layout FROM apps WHERE id=?", id).Scan(&value); err != nil {
		return layout, err
	}
	err := json.Unmarshal([]byte(value), &layout)
	return layout, err
}
func (store *Store) AuthenticateApp(id, secret string) error {
	var expected []byte
	err := store.db.QueryRow("SELECT secret FROM apps WHERE id=?", id).Scan(&expected)
	actual := sha256.Sum256([]byte(secret))
	if err == sql.ErrNoRows || secret == "" || !security.Equal(expected, actual[:]) {
		return domain.Unauthorized("invalid App credentials")
	}
	return err
}
func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.NotFound()
	}
	return nil
}

func (store *Store) SaveApp(app domain.App, create bool, secret string) error {
	if err := store.tabExists(app.TabID); err != nil {
		return err
	}
	if create {
		hash := sha256.Sum256([]byte(secret))
		_, err := store.db.Exec("INSERT INTO apps(id,tab_id,config,secret) VALUES(?,?,?,?)", app.ID, app.TabID, domain.EncodeJSON(app), hash[:])
		return err
	}
	return affected(store.db.Exec("UPDATE apps SET tab_id=?,config=? WHERE id=?", app.TabID, domain.EncodeJSON(app), app.ID))
}
func (store *Store) DeleteApp(id string) error {
	return affected(store.db.Exec("DELETE FROM apps WHERE id=?", id))
}
func (store *Store) ReplicateApp(sourceID string, app domain.App, secret string, layout domain.Layout) error {
	if err := store.tabExists(app.TabID); err != nil {
		return err
	}
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hash := sha256.Sum256([]byte(secret))
	if err := affected(tx.Exec("INSERT INTO apps(id,tab_id,config,secret,layout) SELECT ?,?,?,?,? FROM apps WHERE id=?", app.ID, app.TabID, domain.EncodeJSON(app), hash[:], domain.EncodeJSON(layout), sourceID)); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO streams(app_id,name,definition) SELECT ?,name,definition FROM streams WHERE app_id=?", app.ID, sourceID); err != nil {
		return err
	}
	return tx.Commit()
}
func (store *Store) RotateSecret(id, secret string) error {
	hash := sha256.Sum256([]byte(secret))
	return affected(store.db.Exec("UPDATE apps SET secret=? WHERE id=?", hash[:], id))
}
func (store *Store) SaveLayout(id string, layout domain.Layout) error {
	return affected(store.db.Exec("UPDATE apps SET layout=? WHERE id=?", domain.EncodeJSON(layout), id))
}
func (store *Store) DirectParticipant(id string) (*domain.Instance, error) {
	var direct sql.NullString
	if err := store.db.QueryRow("SELECT direct FROM apps WHERE id=?", id).Scan(&direct); err != nil {
		return nil, err
	}
	if !direct.Valid {
		return nil, nil
	}
	var participant domain.Instance
	if err := domain.StrictJSON([]byte(direct.String), &participant); err != nil {
		return nil, err
	}
	return &participant, nil
}
