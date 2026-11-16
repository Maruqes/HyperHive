package api

import (
	"512SvMan/services"
	"encoding/json"
	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type gpuModeRequest struct {
	GPURef string `json:"gpu_ref"`
	Mode   string `json:"mode"` // none, host or vm
	VMName string `json:"vm_name"`
}

func writePCIError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	msg := strings.ToLower(err.Error())
	status := http.StatusInternalServerError

	switch {
	case strings.Contains(msg, "required"), strings.Contains(msg, "empty"), strings.Contains(msg, "invalid gpu mode"), strings.Contains(msg, "invalid pci address"):
		status = http.StatusBadRequest
	case strings.Contains(msg, "is in use"), strings.Contains(msg, "must be shut down"), strings.Contains(msg, "is held by"):
		status = http.StatusConflict
	case strings.Contains(msg, "not connected"), strings.Contains(msg, "not found"):
		status = http.StatusNotFound
	}

	http.Error(w, err.Error(), status)
}

func writeHostGPUs(w http.ResponseWriter, resp *pciGrpc.HostGPUList) {
	if resp == nil {
		http.Error(w, "GPU inventory response was empty", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(resp.GetGpus()) == 0 {
		_, _ = w.Write([]byte(`{"gpus":[]}`))
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func listHostGPUs(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	svc := services.PCIService{}
	resp, err := svc.ListHostGPUs(r.Context(), machineName)
	if err != nil {
		writePCIError(w, err)
		return
	}

	writeHostGPUs(w, resp)
}

func listHostGPUsWithIOMMU(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	svc := services.PCIService{}
	resp, err := svc.ListHostGPUsWithIOMMU(r.Context(), machineName)
	if err != nil {
		writePCIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func listVMGPUs(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	vmName := strings.TrimSpace(chi.URLParam(r, "vm_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	svc := services.PCIService{}
	resp, err := svc.ListVMGPUs(r.Context(), machineName, vmName)
	if err != nil {
		writePCIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// setGPUMode switches a GPU and answers when the switch is done. On an error the GPU keeps
// its stored mode; the message says why, e.g. which VM or host process still holds it.
func setGPUMode(w http.ResponseWriter, r *http.Request, mode func(gpuModeRequest) gpuModeRequest) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	var req gpuModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req = mode(req)

	svc := services.PCIService{}
	if err := svc.SetGPUMode(r.Context(), machineName, req.GPURef, req.Mode, req.VMName); err != nil {
		writePCIError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]bool{"ok": true})
}

func setGPUModeHandler(w http.ResponseWriter, r *http.Request) {
	setGPUMode(w, r, func(req gpuModeRequest) gpuModeRequest { return req })
}

// The three routes below predate modes and are kept for old clients.
func attachGPUToVM(w http.ResponseWriter, r *http.Request) {
	setGPUMode(w, r, func(req gpuModeRequest) gpuModeRequest { req.Mode = "vm"; return req })
}

func detachGPUFromVM(w http.ResponseWriter, r *http.Request) {
	setGPUMode(w, r, func(req gpuModeRequest) gpuModeRequest { req.Mode, req.VMName = "none", ""; return req })
}

func returnGPUToHost(w http.ResponseWriter, r *http.Request) {
	setGPUMode(w, r, func(req gpuModeRequest) gpuModeRequest { req.Mode, req.VMName = "none", ""; return req })
}

func setupPCIAPI(r chi.Router) chi.Router {
	return r.Route("/pci", func(r chi.Router) {
		r.Get("/host/{machine_name}", listHostGPUs)
		r.Get("/host/iommu/{machine_name}", listHostGPUsWithIOMMU)
		r.Get("/vm/{machine_name}/{vm_name}", listVMGPUs)
		r.Post("/mode/{machine_name}", setGPUModeHandler)
		r.Post("/attach/{machine_name}", attachGPUToVM)
		r.Post("/detach/{machine_name}", detachGPUFromVM)
		r.Post("/return/{machine_name}", returnGPUToHost)
	})
}
