package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

type scheduleRequest struct {
	WeekDay int    `json:"week_day"` // 0=Sunday..6=Saturday
	Hour    int    `json:"hour"`
	Type    string `json:"type"`
	Device  string `json:"device"`
	Active  *bool  `json:"active"` // optional
}

func (s *scheduleRequest) Validate(machineName string) error {
	// Check week day
	if s.WeekDay < 0 || s.WeekDay > 6 {
		return fmt.Errorf("week_day must be 0..6")
	}
	// Check hour
	if s.Hour < 0 || s.Hour > 23 {
		return fmt.Errorf("hour must be 0..23")
	}
	// Check type
	tt := strings.ToLower(strings.TrimSpace(s.Type))
	if tt != "short" && tt != "long" && tt != "extended" && tt != "" {
		return fmt.Errorf("type must be short or extended")
	}
	// Check device exists
	btrfsService := services.BTRFSService{}
	disks, err := btrfsService.GetAllDisks(machineName)
	if err != nil {
		return err
	}
	found := false
	for _, disk := range disks.Disks {
		if disk.Path == s.Device {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("device not found")
	}
	return nil
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
	ctx := keepAliveCtx(r)
	message, err := service.RunSelfTest(ctx, machineName, req.Device, protoType)
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
	ctx := keepAliveCtx(r)
	msg, err := service.CancelSelfTest(ctx, machineName, req.Device)
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
	ctx := keepAliveCtx(r)
	message, err := service.StartFullWipe(ctx, machineName, req.Device)
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
	ctx := keepAliveCtx(r)
	message, err := service.StartNonDestructiveRealloc(ctx, machineName, req.Device)
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
	ctx := keepAliveCtx(r)
	msg, err := service.CancelRealloc(ctx, machineName, req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": msg})
}

// createSchedule handles POST /{machine_name}/schedules
func createSchedule(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	tt := strings.ToLower(strings.TrimSpace(req.Type))
	if tt == "" {
		tt = "short"
	}
	if tt != "short" && tt != "extended" {
		http.Error(w, "type must be short or extended", http.StatusBadRequest)
		return
	}
	device := strings.TrimSpace(req.Device)
	if device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	err := req.Validate(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := db.AddSchedule(r.Context(), time.Weekday(req.WeekDay), req.Hour, tt, device, machineName, active)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"id": id})
}

// listSchedules handles GET /{machine_name}/schedules
func listSchedules(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	schedules, err := db.GetSchedules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var out []map[string]any
	for _, s := range schedules {
		if s.MachineName != machineName {
			continue
		}
		out = append(out, map[string]any{
			"id":           s.ID,
			"week_day":     int(s.WeekDay),
			"hour":         s.Hour,
			"type":         s.TestType,
			"device":       s.Device,
			"active":       s.Active,
			"machine_name": s.MachineName,
			"last_run":     s.LastRun.Time,
		})
	}
	writeJSON(w, out)
}

// editSchedule handles PUT /{machine_name}/schedules/{id}
func editSchedule(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	device := strings.TrimSpace(req.Device)
	if device == "" {
		http.Error(w, "device is required", http.StatusBadRequest)
		return
	}
	tt := strings.ToLower(strings.TrimSpace(req.Type))
	if tt == "" {
		tt = "short"
	}
	if tt != "short" && tt != "extended" {
		http.Error(w, "type must be short or extended", http.StatusBadRequest)
		return
	}

	err = req.Validate(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s, err := db.GetScheduleByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.MachineName != machineName {
		http.Error(w, "schedule does not belong to this machine", http.StatusBadRequest)
		return
	}
	s.WeekDay = time.Weekday(req.WeekDay)
	s.Hour = req.Hour
	s.TestType = tt
	s.Device = device
	if req.Active != nil {
		s.Active = *req.Active
	}
	if err := db.UpdateSchedule(r.Context(), s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "updated"})
}

// deleteSchedule handles DELETE /{machine_name}/schedules/{id}
func deleteSchedule(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s, err := db.GetScheduleByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.MachineName != machineName {
		http.Error(w, "schedule does not belong to this machine", http.StatusBadRequest)
		return
	}
	if err := db.DeleteSchedule(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "deleted"})
}

// enableSchedule handles PUT /{machine_name}/schedules/{id}/enable
func enableSchedule(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	s, err := db.GetScheduleByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.MachineName != machineName {
		http.Error(w, "schedule does not belong to this machine", http.StatusBadRequest)
		return
	}
	if err := db.SetActive(r.Context(), id, body.Active); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "updated"})
}

func setupSmartDiskAPI(r chi.Router) chi.Router {
	return r.Route("/smartdisk", func(r chi.Router) {
		r.Get("/{machine_name}", getSmartDiskInfo)
		r.Post("/{machine_name}/self-test", runSmartDiskSelfTest)
		r.Get("/{machine_name}/self-test/progress", getSmartDiskSelfTestProgress)
		r.Post("/{machine_name}/self-test/cancel", cancelSmartDiskSelfTest)
		// schedule management
		r.Route("/{machine_name}/schedules", func(r chi.Router) {
			r.Post("/", createSchedule)
			r.Get("/", listSchedules)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", editSchedule)
				r.Delete("/", deleteSchedule)
				r.Put("/enable", enableSchedule)
			})
		})
		r.Route("/{machine_name}/realloc", func(r chi.Router) {
			r.Post("/full-wipe", startForceReallocFullWipe)
			r.Post("/non-destructive", startForceReallocNonDestructive)
			r.Get("/status", getForceReallocStatus)
			r.Get("/", listForceReallocStatus)
			r.Post("/cancel", cancelForceRealloc)
		})
	})
}
