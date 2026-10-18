package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"512SvMan/services"
	"github.com/go-chi/chi/v5"
)

func getCelldObservability(w http.ResponseWriter, r *http.Request) {
	value, err := services.CelldFleet.Observability(r.Context(), chi.URLParam(r, "id"))
	celldResponse(w, 200, value, err)
}
func celldObservationQuery(r *http.Request) (services.ObservationQuery, error) {
	v := r.URL.Query()
	q := services.ObservationQuery{Cursor: v.Get("cursor"), Node: v.Get("node"), Workload: v.Get("workload"), Source: v.Get("source"), Severity: strings.ToUpper(v.Get("severity")), Text: v.Get("text")}
	var err error
	for key, target := range map[string]*int64{"from": &q.From, "to": &q.To} {
		if value := v.Get(key); value != "" {
			*target, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				return q, services.ErrCelldInvalid
			}
		}
	}
	if value := v.Get("limit"); value != "" {
		q.Limit, err = strconv.Atoi(value)
		if err != nil {
			return q, services.ErrCelldInvalid
		}
	}
	if value := v.Get("download"); value != "" {
		if value != "true" && value != "false" {
			return q, services.ErrCelldInvalid
		}
		q.Download = value == "true"
	}
	return q, nil
}
func getCelldObservations(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := celldObservationQuery(r)
		if err != nil {
			celldResponse(w, 400, nil, err)
			return
		}
		q.Kind = kind
		if kind == "span" {
			q.TraceID = chi.URLParam(r, "trace_id")
			q.Kind = ""
			if len(q.TraceID) != 32 {
				celldResponse(w, 400, nil, services.ErrCelldInvalid)
				return
			}
		}
		page, err := services.CelldFleet.Observations(r.Context(), chi.URLParam(r, "id"), q)
		if q.Download && err == nil {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Disposition", `attachment; filename="celld-logs.ndjson"`)
			if page.Truncated {
				w.Header().Set("X-Observability-Truncated", "true")
			}
			if page.NextCursor != "" {
				w.Header().Set("X-Next-Cursor", page.NextCursor)
			}
			encoder := json.NewEncoder(w)
			for _, row := range page.Records {
				_ = encoder.Encode(row)
			}
			return
		}
		celldResponse(w, 200, page, err)
	}
}
func getCelldMetrics(w http.ResponseWriter, r *http.Request) {
	q, err := celldObservationQuery(r)
	if err != nil {
		celldResponse(w, 400, nil, err)
		return
	}
	value, err := services.CelldFleet.ObservationMetrics(r.Context(), chi.URLParam(r, "id"), q)
	celldResponse(w, 200, value, err)
}
