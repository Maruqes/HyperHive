package service

import "hyperhive/support/internal/domain"

func (service *Service) History(id string, query domain.HistoryQuery) (domain.History, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	app, err := service.store.App(id, service.now())
	if err != nil {
		return domain.History{}, err
	}
	if query.Stream == "" {
		return domain.History{}, domain.Invalid("stream is required")
	}
	streams, err := service.store.Streams(id)
	if err != nil {
		return domain.History{}, err
	}
	stream, exists := domain.StreamMap(streams)[query.Stream]
	if !exists {
		return domain.History{}, domain.NotFound()
	}
	if query.Limit < 1 || query.Limit > 1000 {
		return domain.History{}, domain.Invalid("limit must be 1–1000")
	}
	if query.Before != nil && query.After != nil {
		return domain.History{}, domain.Invalid("before and after are mutually exclusive")
	}
	if query.Before != nil && *query.Before < 1 || query.After != nil && *query.After < 1 {
		return domain.History{}, domain.Invalid("invalid sample cursor")
	}
	if len(query.Severity) > 0 && stream.Type != "log" {
		return domain.History{}, domain.Invalid("severity only applies to log streams")
	}
	for _, severity := range query.Severity {
		if !domain.ValidSeverity(severity) {
			return domain.History{}, domain.Invalid("invalid severity")
		}
	}
	return service.store.History(app, query, service.now())
}
