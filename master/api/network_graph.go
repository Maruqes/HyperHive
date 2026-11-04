package api

import (
	"net/http"

	"512SvMan/api/npmapi"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

func getNetworkGraph(w http.ResponseWriter, r *http.Request) {
	graph := (&services.NetworkGraphService{}).Build(r.Context(), npmapi.BaseURL(), npmapi.GetTokenFromContext(r))
	writeJSON(w, graph)
}

func getNetworkTraffic(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, (&services.NetworkGraphService{}).Traffic(r.Context()))
}

func setupNetworkGraphAPI(r chi.Router) {
	r.Get("/network/graph", getNetworkGraph)
	r.Get("/network/traffic", getNetworkTraffic)
}
