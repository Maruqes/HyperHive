package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func getUpdates(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machineName")
	extra := services.ExtraService{}
	updates, err := extra.CheckForUpdates(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// get name and machine name
func performUpdate(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machineName")
	var reqBody struct {
		PkgName string `json:"pkgName"`
		Reboot  bool   `json:"reboot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	extra := services.ExtraService{}
	if err := extra.PerformUpdate(machineName, reqBody.PkgName, reqBody.Reboot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func setupExtraAPI(r chi.Router) chi.Router {
	return r.Route("/extra", func(r chi.Router) {
		r.Get("/getUpdates/{machineName}", getUpdates)
		r.Post("/performUpdate/{machineName}", performUpdate)
	})
}
