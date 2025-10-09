package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

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

	suposedIso, err := db.GetIsoByName(req.ISOName)
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if suposedIso != nil {
		http.Error(w, "ISO already exists", http.StatusConflict)
		return
	}

	//find nfs share by id
	nfsShare, err := db.GetNFSShareByID(req.NfsShareID)
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
	_, err = nfsService.DownloadISO(req.URL, req.ISOName, *nfsShare)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if nfsShare.Target[len(nfsShare.Target)-1] == '/' {
		nfsShare.Target = nfsShare.Target[:len(nfsShare.Target)-1]
	}
	isoPath := nfsShare.Target + "/" + req.ISOName

	err = db.AddISO(nfsShare.MachineName, isoPath, req.ISOName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ISO download finished"))
}

func getAllISOs(w http.ResponseWriter, r *http.Request) {
	isos, err := db.GetAllISOs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(isos)
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
	err = db.RemoveISOByID(id)
	if err != nil {
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
