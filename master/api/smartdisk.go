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

func startForceReallocation(w http.ResponseWriter, r *http.Request) {
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
	progress, err := service.StartForceReallocation(r.Context(), machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"device":           progress.GetDevice(),
		"status":           progress.GetStatus(),
		"progressPercent":  progress.GetProgressPercent(),
		"currentBlock":     progress.GetCurrentBlock(),
		"totalBlocks":      progress.GetTotalBlocks(),
		"elapsedTime":      progress.GetElapsedTime(),
		"readErrors":       progress.GetReadErrors(),
		"writeErrors":      progress.GetWriteErrors(),
		"corruptionErrors": progress.GetCorruptionErrors(),
		"message":          progress.GetMessage(),
		"error":            progress.GetError(),
	})
}

func getForceReallocationProgress(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	device := strings.TrimSpace(r.URL.Query().Get("device"))

	service := services.SmartDiskService{}

	// If device is empty, return all active jobs
	if device == "" {
		jobs, err := service.GetAllForceReallocationProgress(machineName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var response []map[string]any
		for _, progress := range jobs {
			response = append(response, map[string]any{
				"device":           progress.GetDevice(),
				"status":           progress.GetStatus(),
				"progressPercent":  progress.GetProgressPercent(),
				"currentBlock":     progress.GetCurrentBlock(),
				"totalBlocks":      progress.GetTotalBlocks(),
				"elapsedTime":      progress.GetElapsedTime(),
				"readErrors":       progress.GetReadErrors(),
				"writeErrors":      progress.GetWriteErrors(),
				"corruptionErrors": progress.GetCorruptionErrors(),
				"message":          progress.GetMessage(),
				"error":            progress.GetError(),
			})
		}
		writeJSON(w, map[string]any{"jobs": response})
		return
	}

	// Otherwise return progress for specific device
	progress, err := service.GetForceReallocationProgress(machineName, device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"device":           progress.GetDevice(),
		"status":           progress.GetStatus(),
		"progressPercent":  progress.GetProgressPercent(),
		"currentBlock":     progress.GetCurrentBlock(),
		"totalBlocks":      progress.GetTotalBlocks(),
		"elapsedTime":      progress.GetElapsedTime(),
		"readErrors":       progress.GetReadErrors(),
		"writeErrors":      progress.GetWriteErrors(),
		"corruptionErrors": progress.GetCorruptionErrors(),
		"message":          progress.GetMessage(),
		"error":            progress.GetError(),
	})
}

func cancelForceReallocation(w http.ResponseWriter, r *http.Request) {
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
	msg, err := service.CancelForceReallocation(r.Context(), machineName, req.Device)
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
		r.Post("/{machine_name}/force-reallocation", startForceReallocation)
		r.Get("/{machine_name}/force-reallocation/progress", getForceReallocationProgress)
		r.Post("/{machine_name}/force-reallocation/cancel", cancelForceReallocation)
	})
}
