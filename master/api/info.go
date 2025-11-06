package api

import (
	"512SvMan/services"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

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

	infoService := &services.InfoService{}

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

	infoService := &services.InfoService{}

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

	infoService := &services.InfoService{}

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

	infoService := &services.InfoService{}

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

type historyRequest struct {
	Hours  *int `json:"hours"`
	Days   *int `json:"days"`
	Weeks  *int `json:"weeks"`
	Months *int `json:"months"`
}

func parseRelativeDuration(value *int, unit time.Duration) time.Duration {
	if value == nil || *value <= 0 {
		return 0
	}
	return time.Duration(*value) * unit
}

func (h *historyRequest) duration() time.Duration {
	var total time.Duration
	total += parseRelativeDuration(h.Hours, time.Hour)
	total += parseRelativeDuration(h.Days, 24*time.Hour)
	total += parseRelativeDuration(h.Weeks, 7*24*time.Hour)
	total += parseRelativeDuration(h.Months, 30*24*time.Hour)
	return total
}

func parseHistoryDuration(r *http.Request) (time.Duration, error) {
	query := r.URL.Query()
	if raw := query.Get("duration"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, err
		}
		if parsed <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return parsed, nil
	}

	total := time.Duration(0)
	durationKeys := []struct {
		Key  string
		Unit time.Duration
	}{
		{"hours", time.Hour},
		{"days", 24 * time.Hour},
		{"weeks", 7 * 24 * time.Hour},
		{"months", 30 * 24 * time.Hour},
	}

	for _, item := range durationKeys {
		raw := query.Get(item.Key)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid %s value: %w", item.Key, err)
		}
		total += parseRelativeDuration(&value, item.Unit)
	}

	if total > 0 {
		return total, nil
	}

	var body historyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if !errors.Is(err, io.EOF) {
			return 0, err
		}
	}
	return body.duration(), nil
}

func respondHistory[T any](machineName string, duration time.Duration, fetch func(string, time.Duration) ([]T, error), w http.ResponseWriter) {
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}
	if duration <= 0 {
		http.Error(w, "duration must be provided either via query param or body (hours/days/weeks/months)", http.StatusBadRequest)
		return
	}

	data, err := fetch(machineName, duration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getCPUHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	infoService := &services.InfoService{}
	respondHistory(machineName, duration, infoService.GetCPUHistory, w)
}

func getMemHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	infoService := &services.InfoService{}
	respondHistory(machineName, duration, infoService.GetMemHistory, w)
}

func getDiskHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	infoService := &services.InfoService{}
	respondHistory(machineName, duration, infoService.GetDiskHistory, w)
}

func getNetworkHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	infoService := &services.InfoService{}
	respondHistory(machineName, duration, infoService.GetNetworkHistory, w)
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
		r.Get("/history/cpu/{machine_name}", getCPUHistory)
		r.Get("/history/mem/{machine_name}", getMemHistory)
		r.Get("/history/disk/{machine_name}", getDiskHistory)
		r.Get("/history/network/{machine_name}", getNetworkHistory)
		r.Post("/stress-cpu/{machine_name}", stressCPU)
		r.Post("/test-ram/{machine_name}", testRamMEM)
	})
}
