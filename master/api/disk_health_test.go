package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

func TestDiskHealthHandlers(t *testing.T) {
	router := chi.NewRouter()
	setupDiskHealthAPI(router)
	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	// Without the database every route is unavailable.
	previous := db.DiskHealthDB
	db.DiskHealthDB = nil
	t.Cleanup(func() { db.DiskHealthDB = previous })
	if rec := do(http.MethodGet, "/diskhealth/"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled GET status %d", rec.Code)
	}

	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	db.DiskHealthDB = fixture
	t.Cleanup(func() { fixture.Close() })
	ctx := context.Background()
	if err := db.CreateDiskHealthTables(ctx); err != nil {
		t.Fatal(err)
	}
	seenAt := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, serial := range []string{"ACTIVE 1", "OLD"} {
		if err := db.UpsertDiskSeen(ctx, db.DiskSeen{Serial: serial, MachineName: "a", SeenAt: seenAt}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertDiskSeen(ctx, db.DiskSeen{Serial: "ACTIVE 1", MachineName: "a", SeenAt: seenAt.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RetireStaleDisks(ctx, seenAt.Add(time.Minute), seenAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	rec := do(http.MethodGet, "/diskhealth/?state=active")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list status %d: %s", rec.Code, rec.Body)
	}
	var list services.DiskHealthList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Disks) != 1 || list.Disks[0].Serial != "ACTIVE 1" {
		t.Fatalf("active list %+v", list.Disks)
	}
	if rec := do(http.MethodGet, "/diskhealth/?state=bogus"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad state status %d", rec.Code)
	}

	if rec := do(http.MethodGet, "/diskhealth/ACTIVE%201?days=7"); rec.Code != http.StatusOK {
		t.Fatalf("GET history status %d: %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/diskhealth/ACTIVE%201?days=0"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad days status %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/diskhealth/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing history status %d", rec.Code)
	}

	if rec := do(http.MethodDelete, "/diskhealth/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("purge missing status %d", rec.Code)
	}
	if rec := do(http.MethodDelete, "/diskhealth/ACTIVE%201"); rec.Code != http.StatusConflict {
		t.Fatalf("purge active status %d", rec.Code)
	}
	if rec := do(http.MethodDelete, "/diskhealth/OLD"); rec.Code != http.StatusOK {
		t.Fatalf("purge retired status %d: %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/diskhealth/OLD"); rec.Code != http.StatusNotFound {
		t.Fatalf("purged disk still readable: %d", rec.Code)
	}
}
