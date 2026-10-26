package sqlite

import "time"

func (store *Store) CleanupBatch(appID string, now time.Time) (int64, error) {
	result, err := store.db.Exec(`DELETE FROM samples WHERE id IN (
 SELECT samples.id FROM samples JOIN apps ON apps.id=samples.app_id
 WHERE samples.app_id=? AND samples.received_at < ? - json_extract(apps.config,'$.retention_days') * ?
 LIMIT 1000
 )`, appID, now.UnixNano(), int64(24*time.Hour))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
