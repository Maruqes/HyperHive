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

type ColumeCreate struct {
	dockerGrpc.VolumeCreateRequest
	NfsID int `json:"nfs_id"`
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

func listVolumes(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	res, err := svc.VolumeList(machine)
	if err != nil {
		logger.Errorf("docker volume list failed: %v", err)
		http.Error(w, "failed to list volumes: "+err.Error(), http.StatusInternalServerError)
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

func volumeCreateBindMount(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req ColumeCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.VolumeCreateBindMount(r.Context(), machine, &req.VolumeCreateRequest, req.NfsID); err != nil {
		logger.Errorf("docker volume create failed: %v", err)
		http.Error(w, "failed to create volume: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func volumeRemove(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req dockerGrpc.VolumeRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.VolumeRemove(machine, &req); err != nil {
		logger.Errorf("docker volume remove failed: %v", err)
		http.Error(w, "failed to remove volume: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func listNetworks(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	res, err := svc.NetworkList(machine)
	if err != nil {
		logger.Errorf("docker network list failed: %v", err)
		http.Error(w, "failed to list networks: "+err.Error(), http.StatusInternalServerError)
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

func networkCreate(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	if err := svc.NetworkCreate(machine, req.Name, req.Type); err != nil {
		logger.Errorf("docker network create failed: %v", err)
		http.Error(w, "failed to create network: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func networkRemove(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req dockerGrpc.NetworkRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.DockerService{}
	if err := svc.NetworkRemove(machine, &req); err != nil {
		logger.Errorf("docker network remove failed: %v", err)
		http.Error(w, "failed to remove network: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func gitClone(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Link        string            `json:"link"`
		FolderToRun string            `json:"folder_to_run"`
		Name        string            `json:"name"`
		Id          string            `json:"id"`
		Env         map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Link == "" {
		http.Error(w, "git link is required", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	err := svc.GitClone(r.Context(), machine, req.Link, req.FolderToRun, req.Name, req.Id, req.Env)
	if err != nil {
		http.Error(w, "git clone failed "+err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func gitList(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	res, err := svc.GitList(machine)
	if err != nil {
		http.Error(w, "git list failed "+err.Error(), http.StatusBadRequest)
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

func gitRemove(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	err := svc.GitRemove(r.Context(), machine, req.Name)
	if err != nil {
		http.Error(w, "git remove failed "+err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func gitUpdate(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machineName")
	if machine == "" {
		http.Error(w, "machine name is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name string            `json:"name"`
		Id   string            `json:"id"`
		Env  map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	svc := services.DockerService{}
	envVars, err := svc.GitUpdate(r.Context(), machine, req.Name, req.Id, req.Env)
	if err != nil {
		http.Error(w, "git update failed "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp, marshalErr := json.Marshal(envVars)
	if marshalErr != nil {
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(resp)
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

		r.Route("/volumes", func(r chi.Router) {
			r.Get("/{machineName}", listVolumes)
			r.Post("/create/{machineName}", volumeCreateBindMount)
			r.Delete("/remove/{machineName}", volumeRemove)
		})

		r.Route("/networks", func(r chi.Router) {
			r.Get("/{machineName}", listNetworks)
			r.Post("/create/{machineName}", networkCreate)
			r.Delete("/remove/{machineName}", networkRemove)
		})

		r.Route("/git", func(r chi.Router) {
			r.Get("/{machineName}", gitList)
			r.Post("/clone/{machineName}", gitClone)
			r.Delete("/remove/{machineName}", gitRemove)
			r.Post("/update/{machineName}", gitUpdate)
		})
	})
}
