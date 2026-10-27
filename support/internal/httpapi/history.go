package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hyperhive/support/internal/domain"
)

func (handler *Handler) history(writer http.ResponseWriter, request *http.Request, id string) error {
	values := request.URL.Query()
	query := domain.HistoryQuery{Stream: values.Get("stream"), Limit: 200}
	if values.Has("limit") {
		limit, err := strconv.Atoi(values.Get("limit"))
		if err != nil {
			return domain.Invalid("limit must be 1–1000")
		}
		query.Limit = limit
	}
	if values.Has("buckets") {
		buckets, err := strconv.Atoi(values.Get("buckets"))
		if err != nil || buckets < 1 || buckets > 400 {
			return domain.Invalid("buckets must be 1–400")
		}
		query.Buckets = buckets
	}
	for _, name := range []string{"before", "after"} {
		if values.Has(name) {
			cursor, err := strconv.ParseInt(values.Get(name), 10, 64)
			if err != nil {
				return domain.Invalid("invalid sample cursor")
			}
			if name == "before" {
				query.Before = &cursor
			} else {
				query.After = &cursor
			}
		}
	}
	for _, name := range []string{"since", "until"} {
		if values.Has(name) {
			value, err := time.Parse(time.RFC3339Nano, values.Get(name))
			if err != nil {
				return domain.Invalid("invalid " + name + " timestamp")
			}
			if name == "since" {
				query.Since = &value
			} else {
				query.Until = &value
			}
		}
	}
	if values.Has("severity") {
		query.Severity = strings.Split(values.Get("severity"), ",")
	}
	page, err := handler.service.History(id, query)
	if err != nil {
		return err
	}
	respond(writer, 200, page)
	return nil
}
