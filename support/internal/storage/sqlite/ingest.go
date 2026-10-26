package sqlite

import (
	"context"
	"time"

	"hyperhive/support/internal/domain"
)

func (store *Store) RecordBatch(ctx context.Context, id string, body domain.IngestRequest, participant domain.Instance, now time.Time) ([]domain.Sample, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback()
	for _, stream := range body.Streams {
		if _, err = transaction.Exec("INSERT INTO streams(app_id,name,definition) VALUES(?,?,?) ON CONFLICT(app_id,name) DO UPDATE SET definition=excluded.definition", id, stream.Name, domain.EncodeJSON(stream)); err != nil {
			return nil, err
		}
	}
	if body.InstanceID != nil {
		_, err = transaction.Exec("INSERT INTO instances(app_id,id,state) VALUES(?,?,?) ON CONFLICT(app_id,id) DO UPDATE SET state=excluded.state", id, *body.InstanceID, domain.EncodeJSON(participant))
	} else {
		_, err = transaction.Exec("UPDATE apps SET direct=? WHERE id=?", domain.EncodeJSON(participant), id)
	}
	if err != nil {
		return nil, err
	}
	samples := make([]domain.Sample, 0, len(body.Samples))
	for _, incoming := range body.Samples {
		timestamp := now
		if incoming.Timestamp != nil {
			timestamp = incoming.Timestamp.UTC()
		}
		result, err := transaction.Exec("INSERT INTO samples(app_id,stream,instance_id,timestamp,received_at,value) VALUES(?,?,?,?,?,?)", id, incoming.Stream, body.InstanceID, timestamp.Format(time.RFC3339Nano), now.UnixNano(), string(incoming.Value))
		if err != nil {
			return nil, err
		}
		sampleID, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		samples = append(samples, domain.Sample{ID: sampleID, AppID: id, Stream: incoming.Stream, InstanceID: body.InstanceID, Timestamp: timestamp, ReceivedAt: now, Value: incoming.Value})
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}

	return samples, nil
}
