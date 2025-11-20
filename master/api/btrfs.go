package api

import (
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func test(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(vp.ServeWS).ServeHTTP(w, r)
}

func writeProtoJSON(w http.ResponseWriter, m proto.Message) {
	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: true,
	}

	b, err := marshaler.Marshal(m)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func getAllDisks(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	// imagina que isto te devolve um *pb.GetAllDisksResponse
	resp, err := btrfsService.GetAllDisks(machineName)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get disks: %v", err), http.StatusInternalServerError)
		return
	}

	writeProtoJSON(w, resp)
}

func getFreeDisks(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	// imagina que isto te devolve um *pb.GetAllDisksResponse
	resp, err := btrfsService.GetNotMountedDisks(machineName)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get disks: %v", err), http.StatusInternalServerError)
		return
	}

	writeProtoJSON(w, resp)
}

func getAllRaids(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	resp, err := btrfsService.GetAllFileSystems(machineName) // *pb.GetAllFileSystemsResponse
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get raids: %v", err), http.StatusInternalServerError)
		return
	}

	writeProtoJSON(w, resp)
}

func createRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type CreateRaidReq struct {
		Name  string   `json:"name"`
		Raid  string   `json:"raid"`
		Disks []string `json:"disks"`
	}

	var req CreateRaidReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.CreateRaid(machineName, req.Name, req.Raid, req.Disks...); err != nil {
		http.Error(w, fmt.Sprintf("failed to create raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func removeRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type RemoveRaidReq struct {
		UUID string `json:"uuid"`
	}

	var req RemoveRaidReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.RemoveRaid(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to create raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func mountRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type MountUUIDREQ struct {
		UUID        string `json:"uuid"`
		MountPoint  string `json:"mount_point"`
		Compression string `json:"compression"`
	}

	var req MountUUIDREQ
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.MountRaid(machineName, req.UUID, req.MountPoint, req.Compression); err != nil {
		http.Error(w, fmt.Sprintf("failed to create raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func umountRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}
	type MountUUIDREQ struct {
		UUID  string `json:"uuid"`
		Force bool   `json:"force"`
	}

	var req MountUUIDREQ
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.UMountRaid(machineName, req.UUID, req.Force); err != nil {
		http.Error(w, fmt.Sprintf("failed to create raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func addAutomaticMount(w http.ResponseWriter, r *http.Request) {
	type AutoMountReq struct {
		MachineName string `json:"machine_name"`
		UUID        string `json:"uuid"`
		MountPoint  string `json:"mount_point"`
		Compression string `json:"compression"`
	}

	var req AutoMountReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	id, err := btrfsService.AddAutomaticMount(req.MachineName, req.UUID, req.MountPoint, req.Compression)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to add automatic mount: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"id":     id,
	})
}

func removeAutomaticMount(w http.ResponseWriter, r *http.Request) {
	type RemoveAutoMountReq struct {
		ID int `json:"id"`
	}

	var req RemoveAutoMountReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.ID <= 0 {
		http.Error(w, "id must be greater than zero", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	rows, err := btrfsService.RemoveAutomaticMount(req.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to remove automatic mount: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"removed": rows,
	})
}

func addDiskToRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type AddDiskReq struct {
		UUID string `json:"uuid"`
		Disk string `json:"disk"`
	}

	var req AddDiskReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.AddDiskToRaid(machineName, req.UUID, req.Disk); err != nil {
		http.Error(w, fmt.Sprintf("failed to add disk to raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func removeDiskFromRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type RemoveDiskReq struct {
		UUID string `json:"uuid"`
		Disk string `json:"disk"`
	}

	var req RemoveDiskReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.RemoveDiskFromRaid(machineName, req.UUID, req.Disk); err != nil {
		http.Error(w, fmt.Sprintf("failed to remove disk from raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func replaceDiskInRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type ReplaceDiskReq struct {
		UUID    string `json:"uuid"`
		OldDisk string `json:"old_disk"`
		NewDisk string `json:"new_disk"`
	}

	var req ReplaceDiskReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.ReplaceDiskInRaid(machineName, req.UUID, req.OldDisk, req.NewDisk); err != nil {
		http.Error(w, fmt.Sprintf("failed to replace disk in raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func changeRaidLevel(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type ChangeRaidLevelReq struct {
		UUID         string `json:"uuid"`
		NewRaidLevel string `json:"new_raid_level"`
	}

	var req ChangeRaidLevelReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.ChangeRaidLevel(machineName, req.UUID, req.NewRaidLevel); err != nil {
		http.Error(w, fmt.Sprintf("failed to change raid level: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func balanceRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.BalanceRaid(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to balance raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func defragmentRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.DefragmentRaid(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to defragment raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func scrubRaid(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.ScrubRaid(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to scrub raid: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func getRaidStats(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		http.Error(w, "uuid query parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	resp, err := btrfsService.GetRaidStats(machineName, uuid)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get raid stats: %v", err), http.StatusInternalServerError)
		return
	}

	writeProtoJSON(w, resp)
}

func pauseBalance(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.PauseBalance(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to pause balance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func resumeBalance(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.ResumeBalance(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to resume balance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func cancelBalance(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	type UUIDReq struct {
		UUID string `json:"uuid"`
	}

	var req UUIDReq
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	btrfsService := services.BTRFSService{}
	if err := btrfsService.CancelBalance(machineName, req.UUID); err != nil {
		http.Error(w, fmt.Sprintf("failed to cancel balance: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func scrubStats(w http.ResponseWriter, r *http.Request) {
	machineName := chi.URLParam(r, "machine_name")
	if machineName == "" {
		http.Error(w, "machine_name parameter is required", http.StatusBadRequest)
		return
	}

	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		http.Error(w, "uuid query parameter is required", http.StatusBadRequest)
		return
	}

	btrfsService := services.BTRFSService{}
	resp, err := btrfsService.ScrubStats(machineName, uuid)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get scrub stats: %v", err), http.StatusInternalServerError)
		return
	}

	writeProtoJSON(w, resp)
}

func setupBTRFS(r chi.Router) chi.Router {
	return r.Route("/btrfs", func(r chi.Router) {
		r.Get("/getFreeDisks/{machine_name}", getFreeDisks)      //discos nao usados em nenhum raid nem estao montados
		r.Get("/getAllDuckingDisks/{machine_name}", getAllDisks) // /dev/o_caralho_do_disco  apenas retorna merdas montadas
		r.Get("/getraids/{machine_name}", getAllRaids)
		r.Post("/createraid/{machine_name}", createRaid)
		r.Delete("/removeraid/{machine_name}", removeRaid)

		r.Post("/mount_raid/{machine_name}", mountRaid)
		r.Post("/umount_raid/{machine_name}", umountRaid)
		r.Post("/automatic_mount", addAutomaticMount)
		r.Delete("/automatic_mount", removeAutomaticMount)

		r.Post("/add_diskraid/{machine_name}", addDiskToRaid)
		r.Post("/remove_diskraid/{machine_name}", removeDiskFromRaid)
		r.Post("/replace_diskraid/{machine_name}", replaceDiskInRaid)

		r.Post("/change_raid_level/{machine_name}", changeRaidLevel)
		r.Post("/balance_raid/{machine_name}", balanceRaid)
		r.Post("/pause_balance/{machine_name}", pauseBalance)
		r.Post("/resume_balance/{machine_name}", resumeBalance)
		r.Post("/cancel_balance/{machine_name}", cancelBalance)
		r.Post("/defragment_raid/{machine_name}", defragmentRaid)
		r.Post("/scrub_raid/{machine_name}", scrubRaid)
		r.Get("/scrub_stats/{machine_name}", scrubStats)

		//gpt missing hehehehe obrigado alto sam
		r.Get("/raid_status/{machine_name}", getRaidStats) // Equivalent to `btrfs filesystem show` + `btrfs device stats`
	})
}
