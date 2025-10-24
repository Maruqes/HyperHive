package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"github.com/go-chi/chi/v5"
)

//no futuro enviar as infos por socket e atualizar no momento :D

func getCpuInfo(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	info, err := infoService.GetCPUInfo(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(info)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func getMemSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	info, err := infoService.GetMemSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(info)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func getDiskSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	info, err := infoService.GetDiskSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(info)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func getNetworkSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	info, err := infoService.GetNetworkSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(info)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func stressCPU(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	var params struct {
		NumVCPU    int32 `json:"numVCPU"`
		NumSeconds int32 `json:"numSeconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	_, err := infoService.StressCPU(r.Context(), machineName, &infoGrpc.StressCPUParams{
		NumVCPU:    params.NumVCPU,
		NumSeconds: params.NumSeconds,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func testRamMEM(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}

	var params struct {
		NumGigs     int32 `json:"numOfGigs"`
		NumOfPasses int32 `json:"numOfPasses"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := infoService.TestRamMEM(r.Context(), machineName, &infoGrpc.TestRamMEMParams{
		NumGigs:     params.NumGigs,
		NumOfPasses: params.NumOfPasses,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func setupInfoAPI(r chi.Router) chi.Router {
	return r.Route("/info", func(r chi.Router) {
		r.Get("/cpu/{machine_name}", getCpuInfo)
		r.Get("/mem/{machine_name}", getMemSummary)
		r.Get("/disk/{machine_name}", getDiskSummary)
		r.Get("/network/{machine_name}", getNetworkSummary)
		r.Post("/stress-cpu/{machine_name}", stressCPU)
		r.Post("/test-ram/{machine_name}", testRamMEM)
	})
}
