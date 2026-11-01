package sqlite

import (
	"database/sql"
	"encoding/json"
	"time"

	"hyperhive/support/internal/domain"
)

func (store *Store) Automations() ([]domain.Automation, error) {
	rows, err := store.db.Query("SELECT config,state FROM automations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	automations := []domain.Automation{}
	for rows.Next() {
		var config, state string
		if err := rows.Scan(&config, &state); err != nil {
			return nil, err
		}
		var automation domain.Automation
		if err := json.Unmarshal([]byte(config), &automation); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(state), &automation.State); err != nil {
			return nil, err
		}
		automations = append(automations, automation)
	}
	return automations, rows.Err()
}

// SaveAutomation stores config and state; the config column never holds state.
func (store *Store) SaveAutomation(automation domain.Automation) error {
	state := automation.State
	state.Snapshot = nil
	automation.State = domain.AutomationState{}
	config, err := json.Marshal(automation)
	if err != nil {
		return err
	}
	_, err = store.db.Exec("INSERT INTO automations(id,config,state) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config,state=excluded.state", automation.ID, string(config), domain.EncodeJSON(state))
	return err
}

func (store *Store) SaveAutomationState(id string, state domain.AutomationState) error {
	state.Snapshot = nil
	_, err := store.db.Exec("UPDATE automations SET state=? WHERE id=?", domain.EncodeJSON(state), id)
	return err
}

func (store *Store) DeleteAutomation(id string) error {
	return affected(store.db.Exec("DELETE FROM automations WHERE id=?", id))
}

func (store *Store) AutomationsPaused() (bool, error) {
	var paused bool
	err := store.db.QueryRow("SELECT paused FROM automation_settings WHERE id=1").Scan(&paused)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return paused, err
}

func (store *Store) SetAutomationsPaused(paused bool) error {
	_, err := store.db.Exec("INSERT INTO automation_settings(id,paused) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET paused=excluded.paused", paused)
	return err
}

// AppIDByName resolves an App by ID or case-insensitive name.
func (store *Store) AppIDByName(name string) (string, error) {
	var id string
	err := store.db.QueryRow("SELECT id FROM apps WHERE id=? OR lower(json_extract(config,'$.name'))=lower(?) ORDER BY id=? DESC LIMIT 1", name, name, name).Scan(&id)
	if err == sql.ErrNoRows {
		return "", domain.NotFound()
	}
	return id, err
}

// LatestValue is the newest sample of one stream from one participant
// (Instance is "" for the direct sender).
type LatestValue struct {
	AppID    string
	Stream   string
	Instance string
	SampleID int64
	Received time.Time
	Value    json.RawMessage
}

// LatestValues returns the newest sample per App, stream and participant.
func (store *Store) LatestValues() ([]LatestValue, error) {
	rows, err := store.db.Query("SELECT id,app_id,stream,COALESCE(instance_id,''),received_at,value FROM samples WHERE id IN (SELECT MAX(id) FROM samples GROUP BY app_id,stream,instance_id)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []LatestValue{}
	for rows.Next() {
		var value LatestValue
		var received int64
		var raw string
		if err := rows.Scan(&value.SampleID, &value.AppID, &value.Stream, &value.Instance, &received, &raw); err != nil {
			return nil, err
		}
		value.Received = time.Unix(0, received).UTC()
		value.Value = json.RawMessage(raw)
		values = append(values, value)
	}
	return values, rows.Err()
}
