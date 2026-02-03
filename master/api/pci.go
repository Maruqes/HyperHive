package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type vmGPURequest struct {
	VMName string `json:"vm_name"`
	GPURef string `json:"gpu_ref"`
}

type gpuReferenceRequest struct {
	GPURef string `json:"gpu_ref"`
}

func writePCIError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	msg := strings.ToLower(err.Error())
	status := http.StatusInternalServerError

	switch {
	case strings.Contains(msg, "required"), strings.Contains(msg, "empty"), strings.Contains(msg, "must be shut down"):
		status = http.StatusBadRequest
	case strings.Contains(msg, "already attached"), strings.Contains(msg, "still attached"):
		status = http.StatusConflict
	case strings.Contains(msg, "not connected"), strings.Contains(msg, "not found"):
		status = http.StatusNotFound
	}

	http.Error(w, err.Error(), status)
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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

func attachGPUToVM(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	var req vmGPURequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.PCIService{}
	resp, err := svc.AttachGPUToVM(r.Context(), machineName, req.VMName, req.GPURef)
	if err != nil {
		writePCIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func detachGPUFromVM(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	var req vmGPURequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.PCIService{}
	resp, err := svc.DetachGPUFromVM(r.Context(), machineName, req.VMName, req.GPURef)
	if err != nil {
		writePCIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func returnGPUToHost(w http.ResponseWriter, r *http.Request) {
	machineName := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	var req gpuReferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	svc := services.PCIService{}
	resp, err := svc.ReturnGPUToHost(r.Context(), machineName, req.GPURef)
	if err != nil {
		writePCIError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func setupPCIAPI(r chi.Router) chi.Router {
	return r.Route("/pci", func(r chi.Router) {
		r.Get("/host/{machine_name}", listHostGPUs)
		r.Get("/host/iommu/{machine_name}", listHostGPUsWithIOMMU)
		r.Get("/vm/{machine_name}/{vm_name}", listVMGPUs)
		r.Post("/attach/{machine_name}", attachGPUToVM)
		r.Post("/detach/{machine_name}", detachGPUFromVM)
		r.Post("/return/{machine_name}", returnGPUToHost)
	})
}
