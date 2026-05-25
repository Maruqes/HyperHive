package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func listVMDisk(w http.ResponseWriter, r *http.Request) {
	service := services.VMDiskService{}
	res, err := service.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func createVMDisk(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Name   string `json:"name"`
		NFSID  int    `json:"nfs_id"`
		SizeGB int64  `json:"size_gb"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	service := services.VMDiskService{}
	res, err := service.Create(r.Context(), reqBody.Name, reqBody.NFSID, reqBody.SizeGB, reqBody.Format)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func deleteVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid vm disk id", http.StatusBadRequest)
		return
	}

	service := services.VMDiskService{}
	res, err := service.Delete(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func growVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid vm disk id", http.StatusBadRequest)
		return
	}

	var reqBody struct {
		SizeGB int64 `json:"size_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	service := services.VMDiskService{}
	res, err := service.Grow(r.Context(), id, reqBody.SizeGB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func setupVMDiskAPI(r chi.Router) chi.Router {
	return r.Route("/vm-disk", func(r chi.Router) {
		r.Get("/list", listVMDisk)
		r.Post("/create", createVMDisk)
		r.Delete("/{id}", deleteVMDisk)
		r.Post("/{id}/grow", growVMDisk)
	})
}
