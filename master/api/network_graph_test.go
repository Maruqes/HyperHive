package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

// Every source fails here (no tables, no NPM), and the route must still answer.
func TestNetworkGraphRouteReturnsPartialGraphWithWarnings(t *testing.T) {
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = fixture
	t.Cleanup(func() { db.DB = previous; fixture.Close() })

	router := chi.NewRouter()
	setupNetworkGraphAPI(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/network/graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var graph services.NetworkGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if graph.GeneratedAt.IsZero() || graph.Nodes == nil || graph.Edges == nil || graph.Flows == nil || len(graph.Warnings) == 0 {
		t.Fatalf("unexpected graph %+v", graph)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/network/traffic", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic status %d: %s", rec.Code, rec.Body)
	}
	var traffic services.NetworkTraffic
	if err := json.Unmarshal(rec.Body.Bytes(), &traffic); err != nil {
		t.Fatal(err)
	}
	if traffic.At.IsZero() || traffic.Nodes == nil || traffic.Endpoints == nil {
		t.Fatalf("unexpected traffic %+v", traffic)
	}
}
