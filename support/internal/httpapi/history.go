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
	if values.Has("since") {
		since, err := time.Parse(time.RFC3339Nano, values.Get("since"))
		if err != nil {
			return domain.Invalid("invalid since timestamp")
		}
		query.Since = &since
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
