package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

func TestPowerScheduleHandlers(t *testing.T) {
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = fixture
	t.Cleanup(func() { db.DB = previous; fixture.Close() })
	if err := db.CreatePowerScheduleTables(context.Background()); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	setupPowerScheduleAPI(router)
	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/power-schedule/", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", rec.Code, rec.Body)
	}
	var view services.PowerScheduleView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Schedule.Enabled || view.Schedule.Start != "08:00" || view.Next != nil {
		t.Fatalf("unexpected default view %+v", view)
	}

	for _, body := range []string{`{`, `{"enabled":true,"start":"08:00","stop":"08:00"}`, `{"enabled":true,"start":"8","stop":"23:00"}`} {
		if rec := do(http.MethodPut, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s status %d, want 400", body, rec.Code)
		}
	}

	if rec := do(http.MethodPut, `{"enabled":true,"start":"08:00","stop":"01:00"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("PUT status %d: %s", rec.Code, rec.Body)
	}
	rec = do(http.MethodGet, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Schedule.Enabled || view.Schedule.Stop != "01:00" || view.Next == nil {
		t.Fatalf("saved view %+v", view)
	}
	if view.Next.MasterStopUnix-view.Next.StopUnix != 180 || view.Next.WakeUnix <= view.Next.MasterStopUnix {
		t.Fatalf("next window %+v", *view.Next)
	}
}
