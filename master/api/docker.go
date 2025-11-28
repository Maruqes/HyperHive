package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

// List returns the docker images for a given machine
func List(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	svc := services.DockerService{}

	res, err := svc.List(machine)
	if err != nil {
		logger.Errorf("docker list failed: %v", err)
		http.Error(w, "failed to list images: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
	if err != nil {
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(data)
}

type downloadReq struct {
	Image    string `json:"image"`
	Registry string `json:"registry"`
}

func download(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")

	var req downloadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.Download(machine, req.Image, req.Registry); err != nil {
		logger.Errorf("docker download failed: %v", err)
		http.Error(w, "failed to download image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type removeReq struct {
	ImageID    string `json:"image_id"`
	Force      bool   `json:"force"`
	PruneChild bool   `json:"prune_child"`
}

func remove(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")

	var req removeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.Remove(machine, req.ImageID, req.Force, req.PruneChild); err != nil {
		logger.Errorf("docker remove failed: %v", err)
		http.Error(w, "failed to remove image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func setupDockerAPI(r chi.Router) chi.Router {
	return r.Route("/docker", func(r chi.Router) {
		r.Route("/images", func(r chi.Router) {
			r.Get("/{machineName}", List)
			r.Post("/download/{machineName}", download)
			r.Delete("/remove/{machineName}", remove)
		})
	})
}
