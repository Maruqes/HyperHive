package api

import (
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

func test(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(vp.ServeWS).ServeHTTP(w, r)
}
func getAllDisks(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	disks, err := btrfsService.GetAllDisks(machineName)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get disks: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(disks); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func getAllRaids(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	raids, err := btrfsService.GetAllFileSystems(machineName)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get disks: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(raids); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func createRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type CreateRaidReq struct {
		Name  string   `json:"name"`
		Raid  string   `json:"raid"`
		Disks []string `json:"disks"`
	}

	var req CreateRaidReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.CreateRaid(machineName, req.Name, req.Raid, req.Disks...); err != nil {
		http.Error(w, fmt.Sprintf("failed to create raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func setupBTRFS(r chi.Router) chi.Router {
	return r.Route("/btrfs", func(r chi.Router) {
		r.Get("/getAllDuckingDisks/{machine_name}", getAllDisks) // /dev/o_caralho_do_disco  apenas retorna merdas montadas
		r.Get("/getraids/{machine_name}", getAllRaids)
		r.Post("/createraid/{machine_name}", createRaid)
		r.Post("/removeraid", test)
		r.Post("/add_diskraid", test)
		r.Post("/remove_diskraid", test)
		r.Post("/replace_diskraid", test)
		r.Post("/change_raid_level", test)
		r.Post("/balance_raid", test)
		r.Post("/defragment_raid", test)
		r.Post("/scrub_raid", test)

		r.Post("/mount_raid", test)
		r.Post("/umount_raid", test)

		//gpt missing hehehehe obrigado alto sam
		r.Get("/raid_status", test) // Equivalent to `btrfs filesystem show` + `btrfs device stats`
		r.Get("/raid_df", test)     // Equivalent to `btrfs filesystem df`
		r.Get("/raid_errors", test) // Parse errors/corruptions
		r.Get("/raid_usage", test)  // Human-friendly summary

	})
}
