package api

import (
	"512SvMan/db"
	"512SvMan/protocol"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var protoJSONMarshaler = protojson.MarshalOptions{
	EmitUnpopulated: true,
}

type historyRequest struct {
	Hours       *int `json:"hours"`
	Days        *int `json:"days"`
	Weeks       *int `json:"weeks"`
	Months      *int `json:"months"`
	NumerOfRows *int `json:"number_of_rows"`
}

type snapshotResponse struct {
	ID          int             `json:"id"`
	MachineName string          `json:"machine_name"`
	CapturedAt  time.Time       `json:"captured_at"`
	Info        json.RawMessage `json:"info"`
}

func writeJSON(w http.ResponseWriter, data any) bool {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func protoToRaw(msg proto.Message) (json.RawMessage, error) {
	if msg == nil {
		return json.RawMessage("null"), nil
	}
	bytes, err := protoJSONMarshaler.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(bytes), nil
}

func writeProto(w http.ResponseWriter, msg proto.Message) bool {
	raw, err := protoToRaw(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(raw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func collectProtoMap(machines []string, fetch func(string) (proto.Message, error)) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(machines))
	for _, name := range machines {
		msg, err := fetch(name)
		if err != nil {
			continue
		}
		raw, err := protoToRaw(msg)
		if err != nil {
			return nil, err
		}
		result[name] = raw
	}
	return result, nil
}

func convertSnapshots(snaps []snapshotCarrier) ([]snapshotResponse, error) {
	out := make([]snapshotResponse, 0, len(snaps))
	for _, snap := range snaps {
		raw, err := protoToRaw(snap.info)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshotResponse{
			ID:          snap.id,
			MachineName: snap.machine,
			CapturedAt:  snap.capturedAt,
			Info:        raw,
		})
	}
	return out, nil
}

type snapshotCarrier struct {
	id         int
	machine    string
	capturedAt time.Time
	info       proto.Message
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
func parseHistoryDuration(r *http.Request) (time.Duration, int, error) {
	query := r.URL.Query()
	if raw := query.Get("duration"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, 0, err
		}
		if parsed <= 0 {
			return 0, 0, fmt.Errorf("duration must be positive")
		}
		return parsed, 0, nil
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
			return 0, 0, fmt.Errorf("invalid %s value: %w", item.Key, err)
		}
		total += parseRelativeDuration(&value, item.Unit)
	}

	if total > 0 {
		return total, 0, nil
	}
	var body historyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if !errors.Is(err, io.EOF) {
			return 0, 0, err
		}
	}
	numberOfRows := 0
	if raw := query.Get("number_of_rows"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid number_of_rows value: %w", err)
		}
		numberOfRows = value
	}
	return body.duration(), numberOfRows, nil
}

// summary handlers

func getCpuInfo(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response, err := collectProtoMap(machines, func(name string) (proto.Message, error) {
			return infoService.GetCPUInfo(name)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	info, err := infoService.GetCPUInfo(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, info)
}

func getMemSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response, err := collectProtoMap(machines, func(name string) (proto.Message, error) {
			return infoService.GetMemSummary(name)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	info, err := infoService.GetMemSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, info)
}

func getDiskSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response, err := collectProtoMap(machines, func(name string) (proto.Message, error) {
			return infoService.GetDiskSummary(name)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	info, err := infoService.GetDiskSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, info)
}

func getNetworkSummary(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response, err := collectProtoMap(machines, func(name string) (proto.Message, error) {
			return infoService.GetNetworkSummary(name)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	info, err := infoService.GetNetworkSummary(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, info)
}

func getProcesses(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}
	processes, err := infoService.GetProcesses(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, processes)
}

func getProcessByPID(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	pidParam := chi.URLParam(r, "pid")
	if pidParam == "" {
		http.Error(w, "pid is required", http.StatusBadRequest)
		return
	}

	pid64, err := strconv.ParseInt(pidParam, 10, 32)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}
	process, err := infoService.GetProcessByPID(machineName, int32(pid64))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, process)
}

func killProcess(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	pidParam := chi.URLParam(r, "pid")
	if pidParam == "" {
		http.Error(w, "pid is required", http.StatusBadRequest)
		return
	}

	pid64, err := strconv.ParseInt(pidParam, 10, 32)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}
	resp, err := infoService.KillProcess(machineName, int32(pid64))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, resp)
}

func terminateProcess(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	pidParam := chi.URLParam(r, "pid")
	if pidParam == "" {
		http.Error(w, "pid is required", http.StatusBadRequest)
		return
	}

	pid64, err := strconv.ParseInt(pidParam, 10, 32)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}
	resp, err := infoService.TerminateProcess(machineName, int32(pid64))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeProto(w, resp)
}

// history handlers

func buildSnapshotCarriers[T any](snaps []T, accessor func(T) snapshotCarrier) []snapshotCarrier {
	res := make([]snapshotCarrier, 0, len(snaps))
	for _, snap := range snaps {
		res = append(res, accessor(snap))
	}
	return res
}

func getCPUHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, numberOfRows, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	if duration <= 0 {
		http.Error(w, "duration must be provided either via query param or body (hours/days/weeks/months)", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response := make(map[string][]snapshotResponse)
		for _, name := range machines {
			snaps, err := infoService.GetCPUHistory(name, duration, numberOfRows)
			if err != nil {
				continue
			}
			carriers := buildSnapshotCarriers(snaps, func(s db.CPUSnapshot) snapshotCarrier {
				return snapshotCarrier{
					id:         s.ID,
					machine:    s.MachineName,
					capturedAt: s.CapturedAt,
					info:       s.Info,
				}
			})
			converted, err := convertSnapshots(carriers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response[name] = converted
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	snaps, err := infoService.GetCPUHistory(machineName, duration, numberOfRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	carriers := buildSnapshotCarriers(snaps, func(s db.CPUSnapshot) snapshotCarrier {
		return snapshotCarrier{
			id:         s.ID,
			machine:    s.MachineName,
			capturedAt: s.CapturedAt,
			info:       s.Info,
		}
	})
	converted, err := convertSnapshots(carriers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, converted)
}

func getMemHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, numberOfRows, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	if duration <= 0 {
		http.Error(w, "duration must be provided either via query param or body (hours/days/weeks/months)", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response := make(map[string][]snapshotResponse)
		for _, name := range machines {
			snaps, err := infoService.GetMemHistory(name, duration, numberOfRows)
			if err != nil {
				continue
			}
			carriers := buildSnapshotCarriers(snaps, func(s db.MemSnapshot) snapshotCarrier {
				return snapshotCarrier{
					id:         s.ID,
					machine:    s.MachineName,
					capturedAt: s.CapturedAt,
					info:       s.Info,
				}
			})
			converted, err := convertSnapshots(carriers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response[name] = converted
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	snaps, err := infoService.GetMemHistory(machineName, duration, numberOfRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	carriers := buildSnapshotCarriers(snaps, func(s db.MemSnapshot) snapshotCarrier {
		return snapshotCarrier{
			id:         s.ID,
			machine:    s.MachineName,
			capturedAt: s.CapturedAt,
			info:       s.Info,
		}
	})
	converted, err := convertSnapshots(carriers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, converted)
}

func getDiskHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, numberOfRows, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	if duration <= 0 {
		http.Error(w, "duration must be provided either via query param or body (hours/days/weeks/months)", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response := make(map[string][]snapshotResponse)
		for _, name := range machines {
			snaps, err := infoService.GetDiskHistory(name, duration, numberOfRows)
			if err != nil {
				continue
			}
			carriers := buildSnapshotCarriers(snaps, func(s db.DiskSnapshot) snapshotCarrier {
				return snapshotCarrier{
					id:         s.ID,
					machine:    s.MachineName,
					capturedAt: s.CapturedAt,
					info:       s.Info,
				}
			})
			converted, err := convertSnapshots(carriers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response[name] = converted
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	snaps, err := infoService.GetDiskHistory(machineName, duration, numberOfRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	carriers := buildSnapshotCarriers(snaps, func(s db.DiskSnapshot) snapshotCarrier {
		return snapshotCarrier{
			id:         s.ID,
			machine:    s.MachineName,
			capturedAt: s.CapturedAt,
			info:       s.Info,
		}
	})
	converted, err := convertSnapshots(carriers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, converted)
}

func getNetworkHistory(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	duration, numberOfRows, err := parseHistoryDuration(r)
	if err != nil {
		http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
		return
	}
	if duration <= 0 {
		http.Error(w, "duration must be provided either via query param or body (hours/days/weeks/months)", http.StatusBadRequest)
		return
	}

	infoService := &services.InfoService{}

	if machineName == "" {
		machines := protocol.GetAllMachineNames()
		response := make(map[string][]snapshotResponse)
		for _, name := range machines {
			snaps, err := infoService.GetNetworkHistory(name, duration, numberOfRows)
			if err != nil {
				continue
			}
			carriers := buildSnapshotCarriers(snaps, func(s db.NetworkSnapshot) snapshotCarrier {
				return snapshotCarrier{
					id:         s.ID,
					machine:    s.MachineName,
					capturedAt: s.CapturedAt,
					info:       s.Info,
				}
			})
			converted, err := convertSnapshots(carriers)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response[name] = converted
		}
		if len(response) == 0 {
			http.Error(w, "no machine data available", http.StatusNotFound)
			return
		}
		writeJSON(w, response)
		return
	}

	snaps, err := infoService.GetNetworkHistory(machineName, duration, numberOfRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	carriers := buildSnapshotCarriers(snaps, func(s db.NetworkSnapshot) snapshotCarrier {
		return snapshotCarrier{
			id:         s.ID,
			machine:    s.MachineName,
			capturedAt: s.CapturedAt,
			info:       s.Info,
		}
	})
	converted, err := convertSnapshots(carriers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, converted)
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

	writeJSON(w, map[string]string{"message": result})
}

func timeSince(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name is required", http.StatusBadRequest)
		return
	}

	infoService := services.InfoService{}
	uptime, err := infoService.GetUpTimeByMachine(machineName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"uptime": uptime.String()})
}

func setupInfoAPI(r chi.Router) chi.Router {
	return r.Route("/info", func(r chi.Router) {
		r.Get("/cpu/{machine_name}", getCpuInfo)
		r.Get("/mem/{machine_name}", getMemSummary)
		r.Get("/disk/{machine_name}", getDiskSummary)
		r.Get("/network/{machine_name}", getNetworkSummary)
		r.Get("/processes/{machine_name}", getProcesses)
		r.Get("/processes/{machine_name}/{pid}", getProcessByPID)
		r.Post("/processes/{machine_name}/{pid}/kill", killProcess)
		r.Post("/processes/{machine_name}/{pid}/terminate", terminateProcess)
		r.Get("/history/cpu/{machine_name}", getCPUHistory)
		r.Get("/history/mem/{machine_name}", getMemHistory)
		r.Get("/history/disk/{machine_name}", getDiskHistory)
		r.Get("/history/network/{machine_name}", getNetworkHistory)
		r.Post("/stress-cpu/{machine_name}", stressCPU)
		r.Post("/test-ram/{machine_name}", testRamMEM)

		r.Get("/time-since/{machine_name}", timeSince)
	})
}
