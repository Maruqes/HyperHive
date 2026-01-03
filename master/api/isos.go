package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

func downloadIso(w http.ResponseWriter, r *http.Request) {
	//parse json body
	var req struct {
		URL        string `json:"url"`
		ISOName    string `json:"iso_name"`
		NfsShareID int    `json:"nfs_share_id"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//chck if iso_name and url finish with .iso
	if len(req.ISOName) < 4 || req.ISOName[len(req.ISOName)-4:] != ".iso" {
		http.Error(w, "iso_name must end with .iso", http.StatusBadRequest)
		return
	}
	if len(req.URL) < 4 || req.URL[len(req.URL)-4:] != ".iso" {
		http.Error(w, "url must end with .iso", http.StatusBadRequest)
		return
	}

	suposedIso, err := db.GetIsoByName(r.Context(), req.ISOName)
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if suposedIso != nil {
		http.Error(w, "ISO already exists", http.StatusConflict)
		return
	}

	//find nfs share by id
	nfsShare, err := db.GetNFSShareByID(r.Context(), req.NfsShareID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if nfsShare == nil {
		http.Error(w, "nfs share not found", http.StatusNotFound)
		return
	}

	//download iso
	nfsService := services.NFSService{}
	if err := nfsService.DownloadISOAsync(req.URL, req.ISOName, *nfsShare); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func getAllISOs(w http.ResponseWriter, r *http.Request) {
	isos, err := db.GetAllISOs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type resStruct struct {
		db.ISO
		AvailableOnSlaves map[string]bool `json:"available_on_slaves"`
	}
	isosRes := make([]resStruct, len(isos))
	for i := range isos {
		isosRes[i] = resStruct{
			ISO:               isos[i],
			AvailableOnSlaves: make(map[string]bool),
		}
	}

	//nsf service
	nfsService := services.NFSService{}
	for i := range isos {
		workingFile, err := nfsService.CanFindFileOrDirOnAllSlaves(isos[i].FilePath)
		if err != nil {
			logger.Errorf("CanFindFileOrDirOnAllSlaves failed: %v", err)
			continue
		}
		isosRes[i].AvailableOnSlaves = workingFile
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(isosRes)
}

func removeISOByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	iso, err := db.GetIsoByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "ISO not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if iso == nil {
		http.Error(w, "ISO not found", http.StatusNotFound)
		return
	}
	if err := os.Remove(iso.FilePath); err != nil && !os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf("failed to remove ISO file: %v", err), http.StatusInternalServerError)
		return
	}
	if err := db.RemoveISOByID(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ISO removed"))
}

func setupISOAPI(r chi.Router) chi.Router {
	return r.Route("/isos", func(r chi.Router) {
		r.Post("/download", downloadIso)
		r.Get("/", getAllISOs)
		r.Delete("/{id}", removeISOByID)
	})
}
