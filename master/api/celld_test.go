package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http/httptest"
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
