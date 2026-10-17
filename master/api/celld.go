package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"
)

func getCelldHealth(w http.ResponseWriter, r *http.Request) {
	health := services.CelldFleet.Health(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !health.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(health)
}
