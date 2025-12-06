package api

import (
	"512SvMan/protocol"
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

type connectionFileRequest struct {
	IP string `json:"ip"`
}

func getTLSSANIps(w http.ResponseWriter, r *http.Request) {
	machines := protocol.GetConnectionsSnapshot()
	if len(machines) == 0 {
		http.Error(w, "no connected machines available", http.StatusServiceUnavailable)
		return
	}

	svc := services.K8sService{}
	var lastErr error
	for _, machine := range machines {
		resp, err := svc.GetTLSSANIps(machine.MachineName)
		if err != nil {
			logger.Debugf("k8s tls sans failed for %s: %v", machine.MachineName, err)
			lastErr = err
			continue
		}

		_ = writeProto(w, resp)
		return
	}

	msg := "none of the machines returned TLS SANs"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	http.Error(w, msg, http.StatusNotFound)
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

	machines := protocol.GetConnectionsSnapshot()
	if len(machines) == 0 {
		http.Error(w, "no connected machines available", http.StatusServiceUnavailable)
		return
	}

	svc := services.K8sService{}
	var lastErr error
	for _, machine := range machines {
		resp, err := svc.GetConnectionFile(machine.MachineName, req.IP)
		if err != nil {
			logger.Debugf("k8s connection file failed for %s: %v", machine.MachineName, err)
			lastErr = err
			continue
		}

		_ = writeProto(w, resp)
		return
	}

	msg := "no connection file returned for any machine"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	http.Error(w, msg, http.StatusNotFound)
}

func setupK8sAPI(r chi.Router) chi.Router {
	return r.Route("/k8s", func(r chi.Router) {
		r.Get("/tls-sans", getTLSSANIps)
		r.Post("/connection-file", getConnectionFile)
	})
}
