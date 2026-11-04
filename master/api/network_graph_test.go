package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNetworkHistoryRouteRejectsAnUnknownRange(t *testing.T) {
	router := chi.NewRouter()
	setupNetworkGraphAPI(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/network/traffic/history?range=2d&key=vm:a:web", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestNetworkLayoutRoutes(t *testing.T) {
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = fixture
	t.Cleanup(func() { db.DB = previous; fixture.Close() })
	if err := db.CreateNetworkLayoutTable(t.Context()); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	setupNetworkGraphAPI(router)
	send := func(method, body string) int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/network/layout", strings.NewReader(body)))
		return rec.Code
	}
	saved := func() map[string]db.NetworkPosition {
		positions, err := db.GetNetworkLayout(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return positions
	}

	for _, body := range []string{`nope`, `{"vm:a":{"x":"left"}}`, `{"":{"x":1,"y":2}}`, `{"vm:a":{"x":1e7,"y":2}}`} {
		if code := send(http.MethodPut, body); code != http.StatusBadRequest {
			t.Fatalf("%s: status %d", body, code)
		}
	}
	if got := saved(); len(got) != 0 {
		t.Fatalf("a rejected request saved %+v", got)
	}

	if code := send(http.MethodPut, `{"vm:a":{"x":1,"y":2}}`); code != http.StatusNoContent {
		t.Fatalf("save status %d", code)
	}
	if got := saved(); got["vm:a"] != (db.NetworkPosition{X: 1, Y: 2}) {
		t.Fatalf("saved %+v", got)
	}

	// The graph is how every page gets the layout.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/network/graph", nil))
	var graph services.NetworkGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if graph.Positions["vm:a"] != (db.NetworkPosition{X: 1, Y: 2}) {
		t.Fatalf("graph positions %+v", graph.Positions)
	}

	if code := send(http.MethodDelete, ""); code != http.StatusNoContent {
		t.Fatalf("reset status %d", code)
	}
	if got := saved(); len(got) != 0 {
		t.Fatalf("after reset %+v", got)
	}
}
