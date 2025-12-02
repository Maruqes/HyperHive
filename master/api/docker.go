package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

// List returns the docker images for a given machine
func List(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	svc := services.DockerService{}

	res, err := svc.ImageList(machine)
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
	if err := svc.ImageDownload(machine, req.Image, req.Registry); err != nil {
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

type containerRemoveReq struct {
	ContainerID string `json:"container_id"`
	Force       bool   `json:"force"`
}

type containerIDReq struct {
	ContainerID string `json:"container_id"`
}

type containerKillReq struct {
	ContainerID string `json:"container_id"`
	Signal      string `json:"signal"`
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
	if err := svc.ImageRemove(machine, req.ImageID, req.Force, req.PruneChild); err != nil {
		logger.Errorf("docker remove failed: %v", err)
		http.Error(w, "failed to remove image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func listContainers(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	res, err := svc.ContainerList(machine)
	if err != nil {
		logger.Errorf("docker container list failed: %v", err)
		http.Error(w, "failed to list containers: "+err.Error(), http.StatusInternalServerError)
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

func ContainerCreateFunc(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req dockerGrpc.ContainerCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body, expected docker ContainerCreate payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.ContainerCreateFunc(machine, &req); err != nil {
		logger.Errorf("docker container create failed: %v", err)
		http.Error(w, "failed to create container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func containerRemove(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req containerRemoveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.ContainerRemove(machine, req.ContainerID, req.Force); err != nil {
		logger.Errorf("docker container remove failed: %v", err)
		http.Error(w, "failed to remove container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func containerStop(w http.ResponseWriter, r *http.Request) {
	handleContainerIDAction(w, r, func(svc *services.DockerService, machine, containerID string) error {
		return svc.ContainerStop(machine, containerID)
	})
}

func containerStart(w http.ResponseWriter, r *http.Request) {
	handleContainerIDAction(w, r, func(svc *services.DockerService, machine, containerID string) error {
		return svc.ContainerStart(machine, containerID)
	})
}

func containerRestart(w http.ResponseWriter, r *http.Request) {
	handleContainerIDAction(w, r, func(svc *services.DockerService, machine, containerID string) error {
		return svc.ContainerRestart(machine, containerID)
	})
}

func containerPause(w http.ResponseWriter, r *http.Request) {
	handleContainerIDAction(w, r, func(svc *services.DockerService, machine, containerID string) error {
		return svc.ContainerPause(machine, containerID)
	})
}

func containerUnpause(w http.ResponseWriter, r *http.Request) {
	handleContainerIDAction(w, r, func(svc *services.DockerService, machine, containerID string) error {
		return svc.ContainerUnpause(machine, containerID)
	})
}
func containerLogs(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		ContainerID string `json:"container_id"`
		Tail        int    `json:"tail"` // e.g. "all" or number of lines as string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.ContainerLogs(r.Context(), machine, req.ContainerID, int32(req.Tail)); err != nil {
		logger.Errorf("docker container logs failed: %v", err)
		http.Error(w, "failed to get container logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func containerKill(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req containerKillReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}

	if req.Signal == "" {
		req.Signal = "SIGKILL"
	}

	svc := services.DockerService{}
	if err := svc.ContainerKill(machine, req.ContainerID, req.Signal); err != nil {
		logger.Errorf("docker container kill failed: %v", err)
		http.Error(w, "failed to kill container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleContainerIDAction(w http.ResponseWriter, r *http.Request, action func(*services.DockerService, string, string) error) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req containerIDReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := action(&svc, machine, req.ContainerID); err != nil {
		logger.Errorf("docker container action failed: %v", err)
		http.Error(w, "container action failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func containerUpdate(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		ContainerID string  `json:"container_id"`
		Memory      int64   `json:"memory"`
		CPUS        float64 `json:"cpus"`
		Restart     string  `json:"restart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.ContainerUpdate(machine, req.ContainerID, req.Memory, req.CPUS, req.Restart); err != nil {
		logger.Errorf("docker container update failed: %v", err)
		http.Error(w, "failed to update container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func containerRename(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		ContainerID string `json:"container_id"`
		NewName     string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}
	if req.NewName == "" {
		http.Error(w, "new_name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.ContainerRename(machine, req.ContainerID, req.NewName); err != nil {
		logger.Errorf("docker container rename failed: %v", err)
		http.Error(w, "failed to rename container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func containerExec(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		ContainerID string   `json:"container_id"`
		Commands    []string `json:"commands"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ContainerID == "" {
		http.Error(w, "container_id is required", http.StatusBadRequest)
		return
	}
	if len(req.Commands) == 0 {
		http.Error(w, "commands are required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.ContainerExec(machine, req.ContainerID, req.Commands); err != nil {
		logger.Errorf("docker container exec failed: %v", err)
		http.Error(w, "failed to exec in container: "+err.Error(), http.StatusInternalServerError)
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

		r.Route("/containers", func(r chi.Router) {
			r.Get("/{machineName}", listContainers)
			r.Post("/create/{machineName}", ContainerCreateFunc)
			r.Delete("/remove/{machineName}", containerRemove)
			r.Post("/stop/{machineName}", containerStop)
			r.Post("/start/{machineName}", containerStart)
			r.Post("/restart/{machineName}", containerRestart)
			r.Post("/pause/{machineName}", containerPause)
			r.Post("/unpause/{machineName}", containerUnpause)
			r.Post("/kill/{machineName}", containerKill)
			r.Post("/logs/{machineName}", containerLogs)
			r.Post("/update/{machineName}", containerUpdate)
			r.Post("/rename/{machineName}", containerRename)
			r.Post("/exec/{machineName}", containerExec)
		})
	})
}
