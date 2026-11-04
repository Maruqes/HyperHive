package api

import (
	"errors"
	"net/http"

	"512SvMan/api/npmapi"
	"512SvMan/services"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

func getNetworkGraph(w http.ResponseWriter, r *http.Request) {
	graph := (&services.NetworkGraphService{}).Build(r.Context(), npmapi.BaseURL(), npmapi.GetTokenFromContext(r))
	writeJSON(w, graph)
}

func getNetworkTraffic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, (&services.NetworkGraphService{}).Traffic(r.Context()))
}

func getNetworkTrafficHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	history, err := (&services.NetworkGraphService{}).History(r.Context(), query.Get("range"), query["key"])
	if errors.Is(err, services.ErrInvalidNetworkHistory) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		logger.Errorf("network history: %v", err)
		http.Error(w, "network history unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, history)
}

func setupNetworkGraphAPI(r chi.Router) {
	r.Get("/network/graph", getNetworkGraph)
	r.Get("/network/traffic", getNetworkTraffic)
	r.Get("/network/traffic/history", getNetworkTrafficHistory)
}
