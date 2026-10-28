package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"512SvMan/db"
	"512SvMan/internal/testnpm"
	"github.com/go-chi/chi/v5"
)

func TestClearNotifications(t *testing.T) {
	t.Setenv("MCP_TOKEN", "client-key")
	t.Chdir(t.TempDir())
	testnpm.State(t, filepath.Join("npm-data"))
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = previous; database.Close() })
	ctx := context.Background()
	if err := db.DbCreateNotsTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.DbCreatePushSubscriptionsTable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO nots (title) VALUES ('first'), ('second'); INSERT INTO push_subscriptions VALUES ('endpoint', 'key', 'auth')`); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Group(func(r chi.Router) { r.Use(authMiddleware); setupNotsAPI(r) })
	request := func(token string) int {
		req := httptest.NewRequest(http.MethodDelete, "/nots/", nil)
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response.Code
	}
	if status := request(""); status != http.StatusUnauthorized {
		t.Fatalf("unauthorized status: %d", status)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM nots`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unauthorized deletion: %d, %v", count, err)
	}
	for i := 0; i < 2; i++ {
		if status := request("Bearer client-key"); status != http.StatusNoContent {
			t.Fatalf("delete status: %d", status)
		}
	}
	if err := database.QueryRow(`SELECT count(*) FROM nots`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("notifications: %d, %v", count, err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM push_subscriptions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("subscriptions: %d, %v", count, err)
	}
	if _, err := database.Exec(`DROP TABLE nots`); err != nil {
		t.Fatal(err)
	}
	if status := request("Bearer client-key"); status != http.StatusInternalServerError {
		t.Fatalf("failure status: %d", status)
	}
}
