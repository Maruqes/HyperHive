package api

import (
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	smartdiskGrpc "github.com/Maruqes/512SvMan/api/proto/smartdisk"
	"github.com/go-chi/chi/v5"
)

type smartDiskSelfTestRequest struct {
	Device string `json:"device"`
	Type   string `json:"type"`
}

type forceReallocRequest struct {
	Device string `json:"device"`
}

func getSmartDiskInfo(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	device := strings.TrimSpace(r.URL.Query().Get("device"))
	if device == "" {
		http.Error(w, "device query parameter is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	info, err := service.GetSmartInfo(machineName, device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writeProto(w, info)
}

func runSmartDiskSelfTest(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")

	var req smartDiskSelfTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	req.Device = strings.TrimSpace(req.Device)
	if req.Device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}

	testType := strings.ToLower(strings.TrimSpace(req.Type))
	if testType == "" {
		testType = "short"
	}

	var protoType smartdiskGrpc.SelfTestType
	switch testType {
	case "short":
		protoType = smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_SHORT
	case "long", "extended":
		protoType = smartdiskGrpc.SelfTestType_SELF_TEST_TYPE_EXTENDED
	default:
		http.Error(w, "type must be short or extended", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	message, err := service.RunSelfTest(r.Context(), machineName, req.Device, protoType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"message": message})
}

func getSmartDiskSelfTestProgress(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	device := strings.TrimSpace(r.URL.Query().Get("device"))
	if device == "" {
		http.Error(w, "device query parameter is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	progress, err := service.GetSelfTestProgress(machineName, device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"device":           progress.GetDevice(),
		"status":           progress.GetStatus(),
		"progressPercent":  progress.GetProgressPercent(),
		"remainingPercent": progress.GetRemainingPercent(),
	})
}

func cancelSmartDiskSelfTest(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")

	var req struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	req.Device = strings.TrimSpace(req.Device)
	if req.Device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	msg, err := service.CancelSelfTest(r.Context(), machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"message": msg})
}

func startForceReallocFullWipe(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	var req forceReallocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	req.Device = strings.TrimSpace(req.Device)
	if req.Device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	message, err := service.StartFullWipe(r.Context(), machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": message})
}

func startForceReallocNonDestructive(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	var req forceReallocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	req.Device = strings.TrimSpace(req.Device)
	if req.Device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	message, err := service.StartNonDestructiveRealloc(r.Context(), machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": message})
}

func getForceReallocStatus(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	device := strings.TrimSpace(r.URL.Query().Get("device"))
	if device == "" {
		http.Error(w, "device query parameter is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	status, err := service.GetReallocStatus(machineName, device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writeProto(w, status)
}

func listForceReallocStatus(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	service := services.SmartDiskService{}
	statuses, err := service.ListReallocStatus(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := &smartdiskGrpc.ForceReallocStatusList{Statuses: statuses}
	_ = writeProto(w, resp)
}

func cancelForceRealloc(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	var req forceReallocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	req.Device = strings.TrimSpace(req.Device)
	if req.Device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}

	service := services.SmartDiskService{}
	msg, err := service.CancelRealloc(r.Context(), machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": msg})
}

func setupSmartDiskAPI(r chi.Router) chi.Router {
	return r.Route("/smartdisk", func(r chi.Router) {
		r.Get("/{machine_name}", getSmartDiskInfo)
		r.Post("/{machine_name}/self-test", runSmartDiskSelfTest)
		r.Get("/{machine_name}/self-test/progress", getSmartDiskSelfTestProgress)
		r.Post("/{machine_name}/self-test/cancel", cancelSmartDiskSelfTest)
		r.Route("/{machine_name}/realloc", func(r chi.Router) {
			r.Post("/full-wipe", startForceReallocFullWipe)
			r.Post("/non-destructive", startForceReallocNonDestructive)
			r.Get("/status", getForceReallocStatus)
			r.Get("/", listForceReallocStatus)
			r.Post("/cancel", cancelForceRealloc)
		})
	})
}
