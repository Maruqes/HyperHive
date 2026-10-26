package service

import (
	"context"
	"log"
	"time"
)

func (service *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cleanupAt := service.now()
	for {
		select {
		case <-ctx.Done():
			service.hub.Close()
			return
		case <-ticker.C:
			service.mu.Lock()
			apps, err := service.store.Apps(service.now())
			if err == nil {
				for _, app := range apps {
					service.publishState(app)
				}
			}
			service.mu.Unlock()
			if err != nil {
				log.Printf("support state sweep failed: %v", err)
			}
			if !service.now().Before(cleanupAt) {
				if err := service.Cleanup(ctx); err != nil {
					log.Printf("support cleanup failed: %v", err)
				}
				cleanupAt = service.now().Add(time.Minute)
			}
		}
	}
}
func (service *Service) Cleanup(ctx context.Context) error {
	service.mu.Lock()
	apps, err := service.store.Apps(service.now())
	service.mu.Unlock()
	if err != nil {
		return err
	}
	for _, app := range apps {
		removed := false
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			count, err := service.CleanupBatch(app.ID)
			if err != nil {
				return err
			}
			if count < 1000 {
				removed = removed || count > 0
				break
			}
			removed = true
		}
		if removed {
			service.changed(app.ID)
		}
	}
	return nil
}

func (service *Service) CleanupBatch(id string) (int64, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.store.CleanupBatch(id, service.now())
}
