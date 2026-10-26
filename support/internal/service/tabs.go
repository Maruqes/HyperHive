package service

import (
	"strings"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/security"
)

func (service *Service) Tabs() ([]domain.Tab, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.store.Tabs()
}
func (service *Service) SaveTab(id, name string) (domain.Tab, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	name = strings.TrimSpace(name)
	if !domain.ValidLength(name, 1, 120) {
		return domain.Tab{}, domain.Invalid("invalid tab name")
	}
	create := id == ""
	if create {
		var err error
		id, err = security.NewToken()
		if err != nil {
			return domain.Tab{}, err
		}
	}
	tab := domain.Tab{ID: id, Name: name}
	if err := service.store.SaveTab(tab, create); err != nil {
		return domain.Tab{}, err
	}
	service.changed("")
	return tab, nil
}
func (service *Service) DeleteTab(id string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.DeleteTab(id); err != nil {
		return err
	}
	service.changed("")
	return nil
}
