package api

import (
	"512SvMan/env512"
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func listConnections(w http.ResponseWriter, r *http.Request) {

	protocolService := services.ProtocolService{}
	connections := protocolService.GetAllConnections()

	type respConn struct {
		Addr        string    `json:"addr"`
		MachineName string    `json:"machineName"`
		LastSeen    time.Time `json:"lastSeen"`
		EntryTime   time.Time `json:"entryTime"`
		Master      bool      `json:"master"`
	}

	resp := make([]respConn, 0, len(connections))
	for _, con := range connections {
		r := respConn{
			Addr:        con.Addr,
			MachineName: con.MachineName,
			LastSeen:    con.LastSeen,
			EntryTime:   con.EntryTime,
			Master:      con.Addr == env512.MASTER_INTERNET_IP,
		}
		resp = append(resp, r)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func setupProtocolAPI(r chi.Router) chi.Router {
	return r.Route("/protocol", func(r chi.Router) {
		r.Get("/list", listConnections)
	})
}
