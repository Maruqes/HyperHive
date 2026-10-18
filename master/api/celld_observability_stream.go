package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"512SvMan/services"
	"github.com/go-chi/chi/v5"
)

func streamCelldObservations(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := celldObservationQuery(r)
		if err != nil || q.Cursor != "" || q.Download {
			celldResponse(w, 400, nil, services.ErrCelldInvalid)
			return
		}
		q.Kind = kind
		now := time.Now().UnixMilli()
		since := now
		if raw := r.URL.Query().Get("since"); raw != "" {
			since, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || since < now-int64(7*24*time.Hour/time.Millisecond) || since > now+60000 {
				celldResponse(w, 400, nil, services.ErrCelldInvalid)
				return
			}
		}
		if q.From == 0 {
			q.From = now - 3600000
		}
		q.From = max(q.From, now-int64(7*24*time.Hour/time.Millisecond))
		since = min(since, now)
		id := chi.URLParam(r, "id")
		// Subscribe before replay, covering commits concurrent with the history read.
		sub, err := services.CelldFleet.SubscribeObservations(id, q)
		if err != nil {
			celldResponse(w, 503, nil, err)
			return
		}
		defer sub.Cancel()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		send := func(event string, value any) error { return writeCelldStreamEvent(w, event, value) }
		if send("ready", map[string]int64{"received_ms": now}) != nil {
			return
		}
		q.ReceivedAfter = since
		q.To = now
		q.Limit = 500
		// A bounded replay is inclusive of the last received millisecond. IDs dedupe
		// records committed in the same millisecond or also queued by the subscription.
		total := 0
		replayBytes := 0
		for {
			page, e := services.CelldFleet.Observations(r.Context(), id, q)
			if e != nil || page.Error != "" {
				_ = send("reconnect", map[string]string{"error": "History temporarily unavailable; reconnecting."})
				return
			}
			encoded, _ := json.Marshal(page)
			if total+len(page.Records) > 2000 || replayBytes+len(encoded) > 2<<20 {
				_ = send("gap", map[string]string{"error": "Reconnect history exceeded the bounded replay. Reload a narrower window to inspect missed logs."})
				return
			}
			if len(page.Records) > 0 {
				if send("history", page) != nil {
					return
				}
			}
			total += len(page.Records)
			replayBytes += len(encoded)
			if page.NextCursor == "" {
				break
			}
			if total >= 2000 || replayBytes >= 2<<20 {
				_ = send("gap", map[string]string{"error": "Reconnect history exceeded the bounded replay. Reload a narrower window to inspect missed logs."})
				return
			}
			q.Cursor = page.NextCursor
		}
		if send("caught_up", map[string]int64{"received_ms": now, "from": q.From, "to": now}) != nil {
			return
		}
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		// Rotate periodically so authentication is revalidated on reconnect.
		expiry := time.NewTimer(5 * time.Minute)
		defer expiry.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-expiry.C:
				return
			case <-sub.Done:
				_ = send("reconnect", map[string]string{"error": "Live stream fell behind; replaying history."})
				return
			case rows := <-sub.Records:
				// JSON escaping can expand protobuf strings sixfold. Keep each
				// live frame below the client's 1 MiB decoding bound.
				for start := 0; start < len(rows); start += 16 {
					if send("observations", services.ObservationPage{Records: rows[start:min(start+16, len(rows))], From: q.From, To: time.Now().UnixMilli(), Incomplete: true}) != nil {
						return
					}
				}
			case <-heartbeat.C:
				if send("heartbeat", map[string]int64{"time_ms": time.Now().UnixMilli()}) != nil {
					return
				}
			}
		}
	}
}

func writeCelldStreamEvent(w http.ResponseWriter, event string, value any) error {
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return controller.Flush()
}
