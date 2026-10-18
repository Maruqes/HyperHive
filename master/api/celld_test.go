package api

import (
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCelldHealthUnavailableJSON(t *testing.T) {
	old := services.CelldFleet
	services.CelldFleet = &services.CelldService{}
	t.Cleanup(func() { services.CelldFleet = old })
	w := httptest.NewRecorder()
	getCelldHealth(w, httptest.NewRequest("GET", "/celld/health", nil))
	if w.Code != 503 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: %d %v", w.Code, w.Header())
	}
	var health services.CelldHealth
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Healthy || health.Storage.Healthy || health.Machines == nil || health.Storage.Error == "" {
		t.Fatalf("invalid unavailable health shape: %s", w.Body.String())
	}
}

func TestCelldFleetAuthenticatedAPI(t *testing.T) {
	old, oldURL := services.CelldFleet, baseURL
	t.Cleanup(func() { services.CelldFleet = old; baseURL = oldURL })
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer auth.Close()
	baseURL = auth.URL
	path := filepath.Join(t.TempDir(), "fleets.json")
	state := `{"schema":2,"root_user":"admin","root_password":"admin-private","master_ip":"127.0.0.1","machines":[{"name":"one"}],"fleets":{}}`
	if err := os.WriteFile(path, []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	services.CelldFleet = services.NewCelldService(path)
	router := chi.NewRouter()
	router.Use(authMiddleware)
	setupCelldAPI(router)
	call := func(method, path, body string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authorized {
			req.Header.Set("Authorization", "Bearer valid")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, row := range [][2]string{{"GET", "/celld/health"}, {"GET", "/celld/fleets"}, {"POST", "/celld/fleets"}, {"GET", "/celld/fleets/f/credentials"}, {"PATCH", "/celld/fleets/f"}, {"DELETE", "/celld/fleets/f"}, {"POST", "/celld/fleets/f/start"}, {"POST", "/celld/fleets/f/stop"}, {"POST", "/celld/fleets/f/restart"}} {
		if w := call(row[0], row[1], "", false); w.Code != 401 {
			t.Fatalf("unprotected route %v: %d", row, w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"name":"a","servers":[]}`, `{"name":"a","servers":["unknown"]}`, `{"name":"a","servers":["one"],"runtime":true}`, `{} {}`} {
		if w := call("POST", "/celld/fleets", body, true); w.Code != 400 {
			t.Fatalf("invalid body accepted: %d %s", w.Code, w.Body)
		}
	}
	w := call("POST", "/celld/fleets", `{"name":"a","servers":["one"]}`, true)
	if w.Code != 202 || w.Header().Get("Location") == "" {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var fleet services.CelldFleetInfo
	if err := json.Unmarshal(w.Body.Bytes(), &fleet); err != nil {
		t.Fatal(err)
	}
	detail := "/celld/fleets/" + fleet.ID
	credentials := call("GET", detail+"/credentials", "", true)
	if credentials.Code != 200 || credentials.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentials caching/status")
	}
	var keys services.CelldCredentials
	_ = json.Unmarshal(credentials.Body.Bytes(), &keys)
	if keys.AccessKey == "" || keys.SecretKey == "" {
		t.Fatal("missing scoped credentials")
	}
	for _, url := range []string{detail, "/celld/fleets", "/celld/health"} {
		response := call("GET", url, "", true)
		for _, secret := range []string{keys.AccessKey, keys.SecretKey, "admin-private"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("%s leaked secret", url)
			}
		}
	}
	if w = call("PATCH", detail, `{"name":"changed"}`, true); w.Code != 409 {
		t.Fatal("in-flight edit accepted")
	}
	if w = call("POST", "/celld/fleets", `{"name":"a","servers":["one"]}`, true); w.Code != 409 {
		t.Fatal("duplicate name accepted")
	}
	if w = call("GET", "/celld/fleets/missing", "", true); w.Code != 404 {
		t.Fatal("missing fleet status")
	}
	// HTTP actions acknowledge durable desired state, without making network calls.
	data, _ := os.ReadFile(path)
	var saved map[string]any
	_ = json.Unmarshal(data, &saved)
	record := saved["fleets"].(map[string]any)[fleet.ID].(map[string]any)
	for _, action := range []string{"start", "stop", "restart", "delete"} {
		record["operation"] = ""
		data, _ = json.Marshal(saved)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		services.CelldFleet = services.NewCelldService(path)
		method, url := "POST", detail+"/"+action
		if action == "delete" {
			method, url = "DELETE", detail
		}
		if w = call(method, url, "", true); w.Code != 202 {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body)
		}
	}
}
