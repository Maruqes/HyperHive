package service

import (
	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/security"
)

func (service *Service) Apps() ([]domain.App, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.store.Apps(service.now())
}
func (service *Service) Detail(id string) (domain.AppDetail, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	app, err := service.store.App(id, service.now())
	if err != nil {
		return domain.AppDetail{}, err
	}
	streams, err := service.store.Streams(id)
	if err != nil {
		return domain.AppDetail{}, err
	}
	layout, err := service.store.Layout(id)
	if err != nil {
		return domain.AppDetail{}, err
	}
	return domain.AppDetail{App: app, Streams: streams, Layout: layout}, nil
}
func (service *Service) SaveApp(id string, body domain.AppSettings) (domain.App, string, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if id != "" && body.TemplateID != "" {
		return domain.App{}, "", domain.Invalid("templates can only be used when creating an App")
	}
	layout, err := domain.TemplateLayout(body.TemplateID)
	if err != nil {
		return domain.App{}, "", err
	}
	if id != "" {
		if _, err := service.store.App(id, service.now()); err != nil {
			return domain.App{}, "", err
		}
		if body.Tags == nil || body.Timeout == nil || body.Retention == nil {
			return domain.App{}, "", domain.Invalid("all App settings are required")
		}
	}
	app := domain.App{ID: id, Name: body.Name, TabID: body.TabID, Tags: body.Tags, Timeout: 120, Retention: 7}
	if body.Timeout != nil {
		app.Timeout = *body.Timeout
	}
	if body.Retention != nil {
		app.Retention = *body.Retention
	}
	if err := domain.NormalizeApp(&app); err != nil {
		return domain.App{}, "", err
	}
	secret := ""
	if id == "" {
		app.ID, err = security.NewToken()
		if err != nil {
			return domain.App{}, "", err
		}
		secret, err = security.NewToken()
		if err != nil {
			return domain.App{}, "", err
		}
	}
	if err := service.store.SaveApp(app, id == "", secret, layout); err != nil {
		return domain.App{}, "", err
	}
	app, err = service.store.App(app.ID, service.now())
	if err != nil {
		return domain.App{}, "", err
	}
	service.changed(app.ID)
	service.publishState(app)
	return app, secret, nil
}

func (service *Service) ReplicateApp(sourceID string, body domain.ReplicateAppSettings) (domain.App, string, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	source, err := service.store.App(sourceID, service.now())
	if err != nil {
		return domain.App{}, "", err
	}
	app := domain.App{Name: body.Name, TabID: source.TabID, Tags: source.Tags, Timeout: source.Timeout, Retention: source.Retention}
	if body.TabID != "" {
		app.TabID = body.TabID
	}
	if body.Tags != nil {
		app.Tags = body.Tags
	}
	if err := domain.NormalizeApp(&app); err != nil {
		return domain.App{}, "", err
	}
	app.ID, err = security.NewToken()
	if err != nil {
		return domain.App{}, "", err
	}
	secret, err := security.NewToken()
	if err != nil {
		return domain.App{}, "", err
	}
	layout, err := service.store.Layout(sourceID)
	if err != nil {
		return domain.App{}, "", err
	}
	layout.Revision = 0
	if err := service.store.ReplicateApp(sourceID, app, secret, layout); err != nil {
		return domain.App{}, "", err
	}
	app, err = service.store.App(app.ID, service.now())
	if err != nil {
		return domain.App{}, "", err
	}
	service.changed(app.ID)
	service.publishState(app)
	return app, secret, nil
}
func (service *Service) DeleteApp(id string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.DeleteApp(id); err != nil {
		return err
	}
	delete(service.states, id)
	service.changed(id)
	return nil
}
func (service *Service) RotateSecret(id string) (string, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	secret, err := security.NewToken()
	if err != nil {
		return "", err
	}
	if err := service.store.RotateSecret(id, secret); err != nil {
		return "", err
	}
	return secret, nil
}
func (service *Service) Streams(id string) ([]domain.Stream, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, err := service.store.App(id, service.now()); err != nil {
		return nil, err
	}
	return service.store.Streams(id)
}
func (service *Service) Layout(id string) (domain.Layout, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, err := service.store.App(id, service.now()); err != nil {
		return domain.Layout{}, err
	}
	return service.store.Layout(id)
}
func (service *Service) SaveLayout(id string, updated domain.Layout) (domain.Layout, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, err := service.store.App(id, service.now()); err != nil {
		return domain.Layout{}, err
	}
	layout, err := service.store.Layout(id)
	if err != nil {
		return domain.Layout{}, err
	}
	streams, err := service.store.Streams(id)
	if err != nil {
		return domain.Layout{}, err
	}
	if err := domain.ValidateLayout(updated, domain.StreamMap(streams)); err != nil {
		return domain.Layout{}, err
	}
	if updated.Revision != layout.Revision {
		return domain.Layout{}, domain.Conflict("layout changed; reload before saving")
	}
	updated.Revision++
	if updated.Widgets == nil {
		updated.Widgets = []domain.Widget{}
	}
	if err := service.store.SaveLayout(id, updated); err != nil {
		return domain.Layout{}, err
	}
	service.changed(id)
	return updated, nil
}
