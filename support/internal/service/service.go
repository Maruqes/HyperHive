package service

import (
	"context"
	"sync"
	"time"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/realtime"
	"hyperhive/support/internal/storage/sqlite"
)

type Service struct {
	store  *sqlite.Store
	hub    *realtime.Hub
	mu     sync.Mutex
	now    func() time.Time
	states map[string]string
}

func New(store *sqlite.Store, hub *realtime.Hub, now func() time.Time) (*Service, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	service := &Service{store: store, hub: hub, now: now, states: map[string]string{}}
	apps, err := store.Apps(now())
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		service.states[app.ID] = domain.EncodeJSON(app)
	}
	return service, nil
}
func (service *Service) Close() error                   { service.hub.Close(); return service.store.Close() }
func (service *Service) Ping(ctx context.Context) error { return service.store.Ping(ctx) }
func (service *Service) changed(id string) {
	frame := map[string]any{"type": "changed"}
	if id != "" {
		frame["app_id"] = id
	}
	service.hub.Publish(frame)
}
func (service *Service) publishState(app domain.App) {
	state := domain.EncodeJSON(app)
	if service.states[app.ID] != state {
		service.states[app.ID] = state
		service.hub.Publish(map[string]any{"type": "state", "app": app})
	}
}
