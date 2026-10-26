package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/realtime"
	"hyperhive/support/internal/service"
	"hyperhive/support/internal/storage/sqlite"
)

func TestConcurrentLayoutEditsThroughService(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "support.db"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := service.New(store, realtime.NewHub(), nil)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	defer application.Close()
	app, secret, err := application.SaveApp("", domain.AppSettings{Name: "Example", TabID: "general"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Ingest(context.Background(), app.ID, secret, domain.IngestRequest{Streams: []domain.Stream{{Name: "cpu", Type: "number"}}}); err != nil {
		t.Fatal(err)
	}
	layout := domain.Layout{Widgets: []domain.Widget{{ID: "cpu", Stream: "cpu", Kind: "line", W: 6, H: 4}}}
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := application.SaveLayout(app.ID, layout)
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	saved, conflicts := 0, 0
	for err := range results {
		var problem *domain.Error
		if err == nil {
			saved++
		} else if errors.As(err, &problem) && problem.Kind == domain.ConflictState {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if saved != 1 || conflicts != 1 {
		t.Fatalf("concurrent edits: saved=%d conflicts=%d", saved, conflicts)
	}
}
