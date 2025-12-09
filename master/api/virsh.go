package api

import (
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/protocol"
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func getCpuFeatures(w http.ResponseWriter, r *http.Request) {
	virshServices := services.VirshService{}
	w.Header().Set("Content-Type", "application/json")

	var slaveNames []string
	for _, raw := range r.URL.Query()["slavesnames"] {
		for _, name := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				slaveNames = append(slaveNames, trimmed)
			}
		}
	}

	features, err := virshServices.GetCpuDisableFeatures(slaveNames)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.Marshal(features)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func createVM(w http.ResponseWriter, r *http.Request) {
	type VMRequest struct {
		MachineName string `json:"machine_name"`
		Name        string `json:"name"`
		Memory      int32  `json:"memory"`
		Vcpu        int32  `json:"vcpu"`
		DiskSizeGB  int32  `json:"disk_sizeGB"`
		IsoID       int    `json:"iso_id"`
		NfsShareId  int    `json:"nfs_share_id"`
		Network     string `json:"network"`
		VNCPassword string `json:"VNC_password"`
		CpuXml      string `json:"cpu_xml"`
		Live        bool   `json:"live"`
		AutoStart   bool   `json:"auto_start"`
		IsWindows   bool   `json:"is_windows"`
	}

	var vmReq VMRequest
	err := json.NewDecoder(r.Body).Decode(&vmReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	if vmReq.Live {
		err = virshServices.CreateLiveVM(vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword, vmReq.CpuXml, vmReq.AutoStart, vmReq.IsWindows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	} else {
		err = virshServices.CreateVM(vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword, "", vmReq.AutoStart, vmReq.IsWindows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("VM created successfully"))
}

func migrateLiveVM(w http.ResponseWriter, r *http.Request) {
	//get origin machine name
	//get destination machine name
	//get vm name
	//get live bool
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	type MigrateRequest struct {
		OriginMachine      string `json:"origin_machine"`
		DestinationMachine string `json:"destination_machine"`
		Live               bool   `json:"live"`
		TimeoutSeconds     int    `json:"timeout"`
	}

	var migReq MigrateRequest
	err := json.NewDecoder(r.Body).Decode(&migReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}

	ctx := keepAliveCtx(r)

	err = virshServices.MigrateVm(ctx, migReq.OriginMachine, migReq.DestinationMachine, vmName, migReq.Live, migReq.TimeoutSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM migrated successfully"))
}

func getAllVms(w http.ResponseWriter, r *http.Request) {

	virshServices := services.VirshService{}
	start := time.Now()
	logger.Infof("getAllVms: start")

	res, _, err := virshServices.GetAllVms()
	if err != nil {
		logger.Infof("getAllVms: GetAllVms error after %s: %v", time.Since(start).Round(time.Millisecond), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	logger.Infof("getAllVms: got %d VMs in %s", len(res), time.Since(start).Round(time.Millisecond))

	opts := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseEnumNumbers:  true,
	}

	beforeLoop := time.Now()
	var wg sync.WaitGroup
	var firstErr error
	var firstErrMu sync.Mutex
	payload := make([]map[string]interface{}, len(res))

	setFirstErr := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		firstErrMu.Unlock()
	}

	processVM := func(idx int, vm services.VmType) {
		loopStart := time.Now()

		autoStart, err := db.DoesAutoStartExist(vm.Name)
		if err != nil {
			logger.Infof("getAllVms: autoStart lookup fail for %s after %s: %v", vm.Name, time.Since(loopStart).Round(time.Millisecond), err)
			setFirstErr(err)
			return
		}

		vmMap := map[string]interface{}{
			"isLive":    vm.IsLive,
			"autoStart": autoStart,
		}

		if vm.Vm != nil {
			raw, err := opts.Marshal(vm.Vm)
			if err != nil {
				logger.Infof("getAllVms: marshal fail for %s after %s: %v", vm.Name, time.Since(loopStart).Round(time.Millisecond), err)
				setFirstErr(err)
				return
			}

			if err := json.Unmarshal(raw, &vmMap); err != nil {
				logger.Infof("getAllVms: unmarshal fail for %s after %s: %v", vm.Name, time.Since(loopStart).Round(time.Millisecond), err)
				setFirstErr(err)
				return
			}
		}
		vmMap["isLive"] = vm.IsLive
		vmMap["autoStart"] = autoStart
		vmMap["novnclink"] = env512.MAIN_LINK + fmt.Sprintf("/novnc/vnc.html?path=/novnc/ws%%3Fvm%%3D%v", vmMap["name"])
		if vm.Vm != nil {
			if nfsID, err := virshServices.GetNfsByVM(vm.Vm); err == nil {
				vmMap["nfs_id"] = nfsID
			}
		}
		logger.Infof("getAllVms: processed %s in %s", vm.Name, time.Since(loopStart).Round(time.Millisecond))

		payload[idx] = vmMap
	}

	for i, vm := range res {
		wg.Add(1)
		go func(idx int, vm services.VmType) {
			defer wg.Done()
			processVM(idx, vm)
		}(i, vm)
	}

	wg.Wait()

	if firstErr != nil {
		http.Error(w, firstErr.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("getAllVms: loop done in %s", time.Since(beforeLoop).Round(time.Millisecond))

	data, err := json.Marshal(payload)
	if err != nil {
		logger.Infof("getAllVms: marshal payload fail after %s: %v", time.Since(start).Round(time.Millisecond), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Infof("getAllVms: responding after %s (payload %d bytes)", time.Since(start).Round(time.Millisecond), len(data))
	w.Write(data)
}

func deleteVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.DeleteVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM deleted successfully"))
}

func startVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	ctx := keepAliveCtx(r)
	err := virshServices.StartVM(ctx, vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM started successfully"))
}

func shutdownVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.ShutdownVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM shutdown successfully"))
}

func forceShutdownVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.ForceShutdownVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM force shutdown successfully"))
}

func restartVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.RestartVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM rebooted successfully"))
}

func getVmByName(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	vm, err := virshServices.GetVmByName(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vm == nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(vm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func editVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	type EditVMRequest struct {
		Memory     int32 `json:"memory,omitempty"`
		Vcpu       int32 `json:"vcpu,omitempty"`
		DiskSizeGB int32 `json:"disk_sizeGB,omitempty"` // Not implemented yet
	}

	var editReq EditVMRequest
	err := json.NewDecoder(r.Body).Decode(&editReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.EditVM(vmName, int(editReq.Vcpu), int(editReq.Memory), int(editReq.DiskSizeGB))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM edited successfully"))
}

func updateCpuXml(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	type UpdateCpuRequest struct {
		MachineName string `json:"machine_name"`
		CpuXml      string `json:"cpu_xml"`
	}

	var cpuReq UpdateCpuRequest
	err := json.NewDecoder(r.Body).Decode(&cpuReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.UpdateCpuXml(cpuReq.MachineName, vmName, cpuReq.CpuXml)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("CPU XML updated successfully"))
}

func getCpuXML(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	type GetCpuXMLRequest struct {
		MachineName string `json:"machine_name"`
	}

	var cpuReq GetCpuXMLRequest
	err := json.NewDecoder(r.Body).Decode(&cpuReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	cpuXml, err := virshServices.GetCpuXML(cpuReq.MachineName, vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(struct {
		CpuXML string `json:"cpu_xml"`
	}{
		CpuXML: cpuXml,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func removeIso(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.RemoveIso(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ISO removed from VM successfully"))
}

func changeVmNetwork(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}
	type Req struct {
		NewNetwork string `json:"new_network"`
	}

	var req Req
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}

	err = virshServices.ChangeNetwork(vmName, req.NewNetwork)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func resumeVm(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.ResumeVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM resumed successfully"))
}

func pauseVm(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.PauseVM(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM paused successfully"))
}
func exportVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	nfsServices := services.NFSService{}
	vm, err := virshServices.GetVmByName(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vm == nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}

	err = nfsServices.SyncNFSSlavesByMachineName(vm.MachineName)
	if err != nil {
		http.Error(w, "cant sync nfs across all slaves", http.StatusInternalServerError)
		return
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		http.Error(w, "VM must be shut off to export", http.StatusBadRequest)
		return
	}

	extension := ".qcow2\""

	w.Header().Set("Content-Disposition", "attachment; filename=\""+vmName+extension)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, vm.DiskPath)
}

const bufSize = 8 << 20 // 8 MiB
func q(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

type VMRequestImport struct {
	Slave_name  string `json:"slave_name"`
	NfsShareId  int    `json:"nfs_share_id"`
	VmName      string `json:"vm_name"`
	Memory      int32  `json:"memory"`
	Vcpu        int32  `json:"vcpu"`
	Network     string `json:"network"`
	VNCPassword string `json:"VNC_password"`
	CpuXML      string `json:"cpu_xml"`
	Live        bool   `json:"live"`
}

func readVMRequest(r *http.Request) (*VMRequestImport, error) {
	var vmReq VMRequestImport
	var err error

	vmReq.Slave_name = q(r, "slave_name")
	vmReq.VmName = q(r, "vm_name")
	if vmReq.Slave_name == "" || vmReq.VmName == "" {
		return nil, fmt.Errorf("slave_name and vm_name are required")
	}

	nfsShareIDStr := q(r, "nfs_share_id")
	if nfsShareIDStr == "" {
		return nil, fmt.Errorf("nfs_share_id is required")
	}
	nfsID, err := strconv.Atoi(nfsShareIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid nfs_share_id: %v", err)
	}
	vmReq.NfsShareId = nfsID

	if s := q(r, "memory"); s != "" {
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
		}
		vmReq.Memory = int32(v)
	}
	if s := q(r, "vcpu"); s != "" {
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid vcpu: %v", err)
		}
		vmReq.Vcpu = int32(v)
	}
	vmReq.Network = q(r, "network")
	vmReq.VNCPassword = q(r, "VNC_password")
	vmReq.CpuXML = q(r, "cpu_xml") // URL-encode on client if it includes <, >, " …
	if s := q(r, "live"); s != "" {
		vmReq.Live = s == "true"
	}

	return &vmReq, nil
}

func importVM(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vmReq, err := readVMRequest(r)
	if err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if r.ContentLength <= 0 {
		http.Error(w, "Content-Length required", http.StatusLengthRequired)
		return
	}

	virshService := services.VirshService{}

	/*
		check
		Slave_name  string
		NfsShareId  int
		VmName      string
	*/
	vm, err := virshService.GetVmByName(vmReq.VmName)
	if err != nil {
		http.Error(w, "error checking existing VMs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if vm != nil {
		http.Error(w, "a VM with this name already exists", http.StatusConflict)
		return
	}

	slaveMachine := protocol.GetConnectionByMachineName(vmReq.Slave_name)
	if slaveMachine == nil {
		http.Error(w, "slave machine not found", http.StatusNotFound)
		return
	}

	//checks if nfsShareId exists also and creates finalFile path
	finalFile, err := virshService.ImportVmHelper(vmReq.NfsShareId, vmReq.VmName)
	if err != nil {
		http.Error(w, "error preparing import: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmp := finalFile + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		http.Error(w, "open error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	buf := make([]byte, bufSize)
	if _, err := io.CopyBuffer(f, r.Body, buf); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = f.Sync()

	if err := os.Rename(tmp, finalFile); err != nil {
		http.Error(w, "finalize error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := keepAliveCtx(r)

	err = virshService.ColdMigrateVm(
		ctx,
		vmReq.Slave_name,
		&grpcVirsh.ColdMigrationRequest{
			VmName:      vmReq.VmName,
			DiskPath:    finalFile,
			Memory:      vmReq.Memory,
			VCpus:       vmReq.Vcpu,
			Network:     vmReq.Network,
			VncPassword: vmReq.VNCPassword,
			CpuXML:      vmReq.CpuXML,
			Live:        vmReq.Live,
		},
	)
	if err != nil {
		http.Error(w, "error creating VM after import: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func backupVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	nfsID := chi.URLParam(r, "nfs_id")
	if nfsID == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	nfsIdInt, err := strconv.Atoi(nfsID)
	if err != nil {
		http.Error(w, "nfs id is not a number, problem atoi", http.StatusBadRequest)
		return
	}

	nfsServices := services.NFSService{}
	virshServices := services.VirshService{}

	vm, err := virshServices.GetVmByName(vmName)
	if err != nil {
		http.Error(w, "cant get vm by name", http.StatusInternalServerError)
	}

	err = nfsServices.SyncNFSSlavesByMachineName(vm.MachineName)
	if err != nil {
		http.Error(w, "cant sync nfs across all slaves", http.StatusInternalServerError)
		return
	}

	err = virshServices.BackupVM(vmName, nfsIdInt, false)
	if err != nil {
		http.Error(w, "was not possible to backup your vm err: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func caniseefileorfolder(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func getAllBackups(w http.ResponseWriter, r *http.Request) {
	virshBackups, err := db.GetAllVirshBackups()
	if err != nil {
		http.Error(w, "error getting all backups "+err.Error(), http.StatusInternalServerError)
		return
	}
	type Res struct {
		DbRes db.VirshBackup `json:"db_res"`
		Live  bool           `json:"live"`
	}

	var res []Res

	for i := 0; i < len(virshBackups); i++ {
		bak := virshBackups[i]
		nres := Res{
			DbRes: bak,
			Live:  caniseefileorfolder(bak.Path),
		}
		res = append(res, nres)
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
	if err != nil {
		http.Error(w, "error getting all backups "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func downloadBackup(w http.ResponseWriter, r *http.Request) {
	backupId := chi.URLParam(r, "backup_id")
	if backupId == "" {
		http.Error(w, "backup_id is required", http.StatusBadRequest)
		return
	}

	backupIdInt, err := strconv.Atoi(backupId)
	if err != nil {
		http.Error(w, "error with backup_id "+err.Error(), http.StatusBadRequest)
		return
	}

	bak, err := db.GetVirshBackupById(backupIdInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if bak == nil {
		http.Error(w, "bak not found", http.StatusNotFound)
		return
	}

	filename := filepath.Base(bak.Path)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, bak.Path)
}

// ask for new vmname ans use import code
func useBackup(w http.ResponseWriter, r *http.Request) {
	//backup_id
	//new_vm_name
	backupId := chi.URLParam(r, "backup_id")
	if backupId == "" {
		http.Error(w, "backup_id is required", http.StatusBadRequest)
		return
	}

	backupIdInt, err := strconv.Atoi(backupId)
	if err != nil {
		http.Error(w, "error on backupId", http.StatusBadRequest)
		return
	}

	var vmReq VMRequestImport
	err = json.NewDecoder(r.Body).Decode(&vmReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	ctx := keepAliveCtx(r)
	err = virshServices.UseBackup(ctx, backupIdInt,
		vmReq.Slave_name, vmReq.NfsShareId,
		&grpcVirsh.ColdMigrationRequest{
			VmName:      vmReq.VmName,
			Memory:      vmReq.Memory,
			VCpus:       vmReq.Vcpu,
			Network:     vmReq.Network,
			VncPassword: vmReq.VNCPassword,
			CpuXML:      vmReq.CpuXML,
			DiskPath:    "", //UseBackup FUNCTION WILL SET THIS
			Live:        vmReq.Live,
		})
	if err != nil {
		http.Error(w, "was not possible to backup your vm err: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteBackup(w http.ResponseWriter, r *http.Request) {
	backupId := chi.URLParam(r, "backup_id")
	if backupId == "" {
		http.Error(w, "backup_id is required", http.StatusBadRequest)
		return
	}

	backupIdInt, err := strconv.Atoi(backupId)
	if err != nil {
		http.Error(w, "error with backup_id "+err.Error(), http.StatusBadRequest)
		return
	}
	virshServices := services.VirshService{}
	err = virshServices.DeleteBackup(backupIdInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func autoStart(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	type msg struct {
		AutoStart bool `json:"auto_start"`
	}

	var m msg
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil && err != io.EOF {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.AutoStart(vmName, m.AutoStart)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func createAutoBak(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		VmName           string   `json:"vm_name"`
		FrequencyDays    int      `json:"frequency_days"`
		MinTime          db.Clock `json:"min_time"`
		MaxTime          db.Clock `json:"max_time"`
		NfsMountId       int      `json:"nfs_mount_id"`
		MaxBackupsRetain int      `json:"max_backups_retain"`
	}

	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err := virshServices.CreateAutoBak(db.AutomaticBackup{
		VmName:           req.VmName,
		FrequencyDays:    req.FrequencyDays,
		MaxTime:          req.MaxTime,
		MinTime:          req.MinTime,
		NfsMountId:       req.NfsMountId,
		MaxBackupsRetain: req.MaxBackupsRetain,
		Enabled:          true,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func getAutoBak(w http.ResponseWriter, r *http.Request) {
	baks, err := db.GetAllAutomaticBackups()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(baks)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}
func updateAutoBak(w http.ResponseWriter, r *http.Request) {
	autoBakId := chi.URLParam(r, "id")
	if autoBakId == "" {
		http.Error(w, "autoBakId is required", http.StatusBadRequest)
		return
	}

	type Req struct {
		VmName           string   `json:"vm_name"`
		FrequencyDays    int      `json:"frequency_days"`
		MinTime          db.Clock `json:"min_time"`
		MaxTime          db.Clock `json:"max_time"`
		NfsMountId       int      `json:"nfs_mount_id"`
		MaxBackupsRetain int      `json:"max_backups_retain"`
	}

	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	autoBakIdInt, err := strconv.Atoi(autoBakId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.UpdateAutoBak(autoBakIdInt, db.AutomaticBackup{
		VmName:           req.VmName,
		FrequencyDays:    req.FrequencyDays,
		MaxTime:          req.MaxTime,
		MinTime:          req.MinTime,
		NfsMountId:       req.NfsMountId,
		MaxBackupsRetain: req.MaxBackupsRetain,
		Enabled:          true,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func deleteAutoBak(w http.ResponseWriter, r *http.Request) {
	autoBakId := chi.URLParam(r, "id")
	if autoBakId == "" {
		http.Error(w, "autoBakId is required", http.StatusBadRequest)
		return
	}

	autoBakIdInt, err := strconv.Atoi(autoBakId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.DeleteAutoBak(autoBakIdInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func enableAutoBak(w http.ResponseWriter, r *http.Request) {
	autoBakId := chi.URLParam(r, "id")
	if autoBakId == "" {
		http.Error(w, "autoBakId is required", http.StatusBadRequest)
		return
	}

	autoBakIdInt, err := strconv.Atoi(autoBakId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.EnableAutoBak(autoBakIdInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func disableAutoBak(w http.ResponseWriter, r *http.Request) {
	autoBakId := chi.URLParam(r, "id")
	if autoBakId == "" {
		http.Error(w, "autoBakId is required", http.StatusBadRequest)
		return
	}

	autoBakIdInt, err := strconv.Atoi(autoBakId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.DisableAutoBak(autoBakIdInt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func moveDisk(w http.ResponseWriter, r *http.Request) {
	vm_name := chi.URLParam(r, "vm_name")
	if vm_name == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	dest_nfs := chi.URLParam(r, "dest_nfs")
	if dest_nfs == "" {
		http.Error(w, "dest_nfs is required", http.StatusBadRequest)
		return
	}

	// parse optional new_name from JSON body
	type MoveReq struct {
		NewName string `json:"new_name"`
	}
	var mr MoveReq
	if err := json.NewDecoder(r.Body).Decode(&mr); err != nil && err != io.EOF {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(mr.NewName)

	// convert dest_nfs to int
	destNfs, err := strconv.Atoi(dest_nfs)
	if err != nil {
		http.Error(w, "invalid dest_nfs: "+err.Error(), http.StatusBadRequest)
		return
	}

	virshService := services.VirshService{}
	ctx := keepAliveCtx(r)
	err = virshService.MoveDisk(ctx, vm_name, destNfs, newName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func coldMigrate(w http.ResponseWriter, r *http.Request) {
	vm_name := chi.URLParam(r, "vm_name")
	if vm_name == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	dest_machine_name := chi.URLParam(r, "dest_machine_name")
	if dest_machine_name == "" {
		http.Error(w, "dest_machine_name is required", http.StatusBadRequest)
		return
	}

	virshService := services.VirshService{}
	ctx := keepAliveCtx(r)
	err := virshService.ColdMigrate(ctx, vm_name, dest_machine_name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func cloneVM(w http.ResponseWriter, r *http.Request) {
	vm_name := chi.URLParam(r, "vm_name")
	if vm_name == "" {
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	dest_machine_name := chi.URLParam(r, "dest_machine_name")
	if dest_machine_name == "" {
		http.Error(w, "dest_machine_name is required", http.StatusBadRequest)
		return
	}

	dest_nfs := chi.URLParam(r, "dest_nfs")
	if dest_nfs == "" {
		http.Error(w, "dest_nfs is required", http.StatusBadRequest)
		return
	}

	// parse new name from JSON body
	type CloneRequest struct {
		NewName string `json:"new_name"`
	}
	var creq CloneRequest
	if err := json.NewDecoder(r.Body).Decode(&creq); err != nil && err != io.EOF {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(creq.NewName)

	// convert dest_nfs to int
	destNfs, err := strconv.Atoi(dest_nfs)
	if err != nil {
		http.Error(w, "invalid dest_nfs: "+err.Error(), http.StatusBadRequest)
		return
	}

	virshService := services.VirshService{}
	ctx := keepAliveCtx(r)
	err = virshService.CloneVM(ctx, vm_name, newName, dest_machine_name, destNfs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// LEMBRETE VM CREATION TEM DE CRIAR A PROPRIA PASTA, INDEPENDENTEMENTE SE É IMPORT LIVE NORMAL
/*
apis que criam vms

/createvm
/createlivevm

devem conseguir criar live/normal
/import
/useBackup


Temos 3 funcoes que criam vms
	virsh.CreateVM           cria vm normal
	virsh.CreateLiveVM       cria vm normal mas com live
	virsh.ColdMigrateVm      cria vm a partir de um qcow existente
*/

func setupVirshAPI(r chi.Router) chi.Router {
	return r.Route("/virsh", func(r chi.Router) {
		r.Get("/getcpudisablefeatures", getCpuFeatures)
		r.Get("/getallvms", getAllVms)
		r.Post("/createvm", createVM)

		r.Post("/migratevm/{vm_name}", migrateLiveVM)
		r.Post("/updatecpuxml/{vm_name}", updateCpuXml)
		r.Get("/cpuxml/{vm_name}", getCpuXML)

		//controll
		r.Delete("/deletevm/{vm_name}", deleteVM)
		r.Post("/startvm/{vm_name}", startVM)
		r.Post("/shutdownvm/{vm_name}", shutdownVM)
		r.Post("/forceshutdownvm/{vm_name}", forceShutdownVM)
		r.Post("/restartvm/{vm_name}", restartVM)
		r.Post("/editvm/{vm_name}", editVM)
		r.Post("/pausevm/{vm_name}", pauseVm)
		r.Post("/resumevm/{vm_name}", resumeVm)
		r.Get("/getvmbyname/{vm_name}", getVmByName)
		r.Post("/removeiso/{vm_name}", removeIso)

		r.Post("/change_vm_network/{vm_name}", changeVmNetwork)

		//move
		r.Post("/moveDisk/{vm_name}/{dest_nfs}", moveDisk)
		r.Post("/coldMigrate/{vm_name}/{dest_machine_name}", coldMigrate)
		r.Post("/cloneVM/{vm_name}/{dest_nfs}/{dest_machine_name}", cloneVM)

		//export/import
		r.Get("/export/{vm_name}", exportVM)
		r.Put("/import", importVM)

		//backups
		r.Post("/backup/{vm_name}/{nfs_id}", backupVM)
		r.Get("/backups", getAllBackups)
		r.Get("/downloadbackup/{backup_id}", downloadBackup)
		r.Post("/useBackup/{backup_id}", useBackup)
		r.Delete("/deleteBackup/{backup_id}", deleteBackup)

		r.Post("/autostart/{vm_name}", autoStart)

		//automatic backups
		r.Post("/autobak", createAutoBak)
		r.Get("/autobak", getAutoBak)
		r.Put("/autobak/{id}", updateAutoBak)
		r.Delete("/autobak/{id}", deleteAutoBak)
		r.Patch("/autopak/enable/{id}", enableAutoBak)
		r.Patch("/autopak/disable/{id}", disableAutoBak)
	})
}

/*
VM STATE REFERENCE (libvirt / gRPC mapping)
-------------------------------------------

VmState_NOSTATE
    → VM has no defined state (initial, unknown, or transitional).
    → Usually seen after libvirt reconnects or before VM is defined.

VmState_RUNNING
    → VM is currently running and executing normally.
    → Guest OS is active and CPUs are working.

VmState_BLOCKED
    → VM is running but blocked on I/O.
    → Common during heavy disk or network operations.
    → Not an error; usually temporary.

VmState_PAUSED
    → VM execution is paused (manually or by the hypervisor).
    → CPU stopped, RAM kept in memory.
    → Can be resumed instantly.

VmState_SHUTDOWN
    → VM is in the process of shutting down.
    → Guest OS is still running, completing its power-off sequence.
    → Temporary transition before SHUTOFF.

VmState_SHUTOFF
    → VM is completely powered off.
    → No CPU, no memory, process fully stopped.
    → Safe for migration, cloning, or deletion.

VmState_CRASHED
    → VM crashed unexpectedly (e.g., kernel panic or QEMU failure).
    → Process exited abnormally, requires manual restart.

VmState_PMSUSPENDED
    → VM is suspended/hibernated via power management.
    → CPU paused, memory contents saved (to disk or RAM snapshot).
    → Can be resumed later to same state.

VmState_UNKNOWN (if defined in your enum)
    → State could not be determined (e.g., communication failure).
    → Should be treated carefully — recheck or skip operations.

-------------------------------------------
Typical usage examples:

// 1. Check if VM is safe for cold migration
if vm.State != VmState_SHUTOFF {
    return fmt.Errorf("VM must be powered off before migration")
}

// 2. Allow off or suspended as safe states
if vm.State != VmState_SHUTOFF && vm.State != VmState_PMSUSPENDED {
    return fmt.Errorf("VM must be off or suspended")
}

// 3. Detect if VM crashed
if vm.State == VmState_CRASHED {
    log.Warnf("VM %s crashed unexpectedly", vm.Name)
}

-------------------------------------------
Notes:
    • NOSTATE      → undefined / unknown
    • RUNNING      → active, executing
    • BLOCKED      → waiting on I/O
    • PAUSED       → stopped temporarily
    • SHUTDOWN     → shutting down (transitional)
    • SHUTOFF      → fully off
    • CRASHED      → aborted unexpectedly
    • PMSUSPENDED  → hibernated (can resume)
*/
