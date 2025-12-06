package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type connectionFileRequest struct {
	IP string `json:"ip"`
}

func getTLSSANIps(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machineName")
	if machineName == "" {
		http.Error(w, "machineName parameter is required", http.StatusBadRequest)
		return
	}

	svc := services.K8sService{}
	resp, err := svc.GetTLSSANIps(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writeProto(w, resp)
}

func getConnectionFile(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machineName")
	if machineName == "" {
		http.Error(w, "machineName parameter is required", http.StatusBadRequest)
		return
	}

	var req connectionFileRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.IP = strings.TrimSpace(req.IP)
	if req.IP == "" {
		http.Error(w, "ip field is required", http.StatusBadRequest)
		return
	}

	svc := services.K8sService{}
	resp, err := svc.GetConnectionFile(machineName, req.IP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writeProto(w, resp)
}

func setupK8sAPI(r chi.Router) chi.Router {
	return r.Route("/k8s", func(r chi.Router) {
		r.Get("/tls-sans/{machineName}", getTLSSANIps)
		r.Post("/connection-file/{machineName}", getConnectionFile)
	})
}
