package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listConnections(w http.ResponseWriter, r *http.Request) {

	protocolService := services.ProtocolService{}
	connections := protocolService.GetAllConnections()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(connections)
}

func setupProtocolAPI(r chi.Router) chi.Router {
	return r.Route("/protocol", func(r chi.Router) {
		r.Get("/list", listConnections)
	})
}
