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

	type response struct {
		Device           string `json:"device"`
		Status           string `json:"status"`
		ProgressPercent  int64  `json:"progressPercent"`
		RemainingPercent int64  `json:"remainingPercent"`
		TestType         string `json:"testType"`
	}

	out := response{
		Device:           progress.GetDevice(),
		Status:           progress.GetStatus(),
		ProgressPercent:  progress.GetProgressPercent(),
		RemainingPercent: progress.GetRemainingPercent(),
		TestType:         progress.GetTestType(),
	}

	writeJSON(w, out)
}

func setupSmartDiskAPI(r chi.Router) chi.Router {
	return r.Route("/smartdisk", func(r chi.Router) {
		r.Get("/{machine_name}", getSmartDiskInfo)
		r.Post("/{machine_name}/self-test", runSmartDiskSelfTest)
		r.Get("/{machine_name}/self-test/progress", getSmartDiskSelfTestProgress)
	})
}
