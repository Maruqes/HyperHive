package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type powerScheduleRequest struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	Stop    string `json:"stop"`
}

func getPowerSchedule(w http.ResponseWriter, r *http.Request) {
	service := services.PowerScheduleService{}
	view, err := service.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, view)
}

// putPowerSchedule saves the cluster-wide schedule and starts applying it to all
// connected hosts in the background; per-host results appear in GET.
func putPowerSchedule(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req powerScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := services.ValidatePowerSchedule(req.Start, req.Stop); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	service := services.PowerScheduleService{}
	schedule, err := service.Save(r.Context(), req.Enabled, req.Start, req.Stop)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]any{
		"status":   "applying",
		"schedule": schedule,
	})
}

func setupPowerScheduleAPI(r chi.Router) chi.Router {
	return r.Route("/power-schedule", func(r chi.Router) {
		r.Get("/", getPowerSchedule)
		r.Put("/", putPowerSchedule)
	})
}
