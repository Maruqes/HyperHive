package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"512SvMan/automations"
	"512SvMan/db"

	"github.com/go-chi/chi/v5"
)

const automationTestSecret = "0123456789abcdef0123456789abcdef"

// fakeSupport serves the HH Support automation routes the master facade uses.
func fakeSupport(t *testing.T) *httptest.Server {
	t.Helper()
	configs := map[string]automations.Config{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-HH-Support-Key") != automationTestSecret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/automations/")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/automations":
			list := automations.ConfigList{Automations: []automations.Config{}}
			for _, config := range configs {
				list.Automations = append(list.Automations, config)
			}
			_ = json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPost && r.URL.Path == "/automations":
			var config automations.Config
			_ = json.NewDecoder(r.Body).Decode(&config)
			if config.Name == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"name is required"}`))
				return
			}
			config.ID, config.Timeout, config.State = "auto-api", 20, json.RawMessage(`{}`)
			configs[config.ID] = config
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(config)
		case r.Method == http.MethodGet && id == "catalog":
			_, _ = w.Write([]byte(`{"apps":[{"id":"a1","name":"DB","streams":[{"name":"cpu","type":"number"}]}]}`))
		case r.Method == http.MethodGet:
			config, ok := configs[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"not found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(config)
		case r.Method == http.MethodDelete:
			delete(configs, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAutomationHandlers(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	previous := db.DB
	db.DB = fixture
	t.Cleanup(func() { db.DB = previous; fixture.Close() })
	if err := db.CreateAutomationTables(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HH_AUTOMATIONS_DIR", t.TempDir())
	t.Setenv("SUPPORT_URL", fakeSupport(t).URL)
	t.Setenv("SUPPORT_PROXY_SECRET", automationTestSecret)

	router := chi.NewRouter()
	setupAutomationsAPI(router)
	setupSupportInternalAPI(router)
	configureAutomationSDK(router, "9595")
	do := func(method, path, body string, header map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for key, value := range header {
			req.Header.Set(key, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, "/automations/", `{"name":""}`, nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("invalid create: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/automations/catalog", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"cpu"`) {
		t.Fatalf("catalog: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/automations/", `{"name":"Check"}`, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	rec := do(http.MethodGet, "/automations/auto-api/files", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "main.ts") {
		t.Fatalf("files: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPut, "/automations/auto-api/files/content?path=../x.ts", `{"content":"x"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("escaping path: %d", rec.Code)
	}
	if rec := do(http.MethodPut, "/automations/auto-api/files/content?path=main.ts", `{"content":"log.info('hello', ctx.trigger);"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPut, "/automations/auto-api/files/content?path=util.ts", `{"content":"export const x = 1;"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("write helper: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/automations/auto-api/files/rename", `{"from":"util.ts","to":"lib/util.ts"}`, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "lib/util.ts") {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/automations/auto-api/files/rename", `{"from":"lib/util.ts","to":"main.ts"}`, nil); rec.Code != http.StatusConflict {
		t.Fatalf("rename over existing: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/automations/sdk/hh.d.ts", "", nil); !strings.Contains(rec.Body.String(), "getById") {
		t.Fatalf("sdk: %s", rec.Body)
	}

	fire := `{"automation_id":"auto-api","automation_name":"Check","trigger":"condition","condition":"x","snapshot":{"db.cpu":97},"timeout_seconds":20}`
	if rec := do(http.MethodPost, "/support-internal/automations/fire", fire, map[string]string{"X-HH-Support-Key": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("fire without secret: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/support-internal/automations/fire", fire, map[string]string{"X-HH-Support-Key": automationTestSecret})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("fire: %d %s", rec.Code, rec.Body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &started)
	var run db.AutomationRun
	for deadline := time.Now().Add(20 * time.Second); ; {
		rec = do(http.MethodGet, "/automations/runs/"+started.RunID, "", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &run)
		if run.Status != automations.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if run.Status != automations.StatusSucceeded || run.Output == nil || !strings.Contains(*run.Output, "[info] hello condition") || run.Snapshot["db.cpu"] != 97.0 {
		t.Fatalf("run: %+v output=%v", run, run.Output)
	}
	rec = do(http.MethodGet, "/automations", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"last_run":{"id":"`+started.RunID) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/automations/runs/"+started.RunID+"/kill", "", nil); rec.Code != http.StatusConflict {
		t.Fatalf("kill finished run: %d", rec.Code)
	}
	if rec := do(http.MethodDelete, "/automations/auto-api", "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/automations/runs?automation_id=auto-api", "", nil); strings.Contains(rec.Body.String(), started.RunID) {
		t.Fatalf("runs survived delete: %s", rec.Body)
	}
}
