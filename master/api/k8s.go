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
	svc := services.K8sService{}
	resp, err := svc.GetTLSSANIpsAny()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writeProto(w, resp)
}

func getConnectionFile(w http.ResponseWriter, r *http.Request) {
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
	resp, err := svc.GetConnectionFileAny(req.IP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=connection-file")
	_, _ = w.Write([]byte(resp.File))
}

func getClusterStatus(w http.ResponseWriter, r *http.Request) {
	svc := services.K8sService{}
	status, err := svc.GetClusterStatus()
	if err != nil && (status == nil || len(status.Connected) == 0) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func setupK8sAPI(r chi.Router) chi.Router {
	return r.Route("/k8s", func(r chi.Router) {
		r.Get("/tls-sans", getTLSSANIps)
		r.Get("/cluster/status", getClusterStatus)
		r.Post("/connection-file", getConnectionFile)
	})
}
