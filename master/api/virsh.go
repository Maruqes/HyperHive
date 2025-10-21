package api

import (
	"512SvMan/db"
	"512SvMan/protocol"
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/go-chi/chi/v5"
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
	}

	var vmReq VMRequest
	err := json.NewDecoder(r.Body).Decode(&vmReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.CreateVM(vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("VM created successfully"))
}

func getAllVms(w http.ResponseWriter, r *http.Request) {

	virshServices := services.VirshService{}

	res, _, err := virshServices.GetAllVms()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
	err := virshServices.StartVM(vmName)
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

func createLiveVM(w http.ResponseWriter, r *http.Request) {
	type VMLiveRequest struct {
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
	}

	var vmReq VMLiveRequest
	err := json.NewDecoder(r.Body).Decode(&vmReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.CreateLiveVM(vmReq.MachineName, vmReq.Name, vmReq.Memory, vmReq.Vcpu, vmReq.NfsShareId, vmReq.DiskSizeGB, vmReq.IsoID, vmReq.Network, vmReq.VNCPassword, vmReq.CpuXml)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Live VM created successfully"))
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
	err = virshServices.MigrateVm(r.Context(), migReq.OriginMachine, migReq.DestinationMachine, vmName, migReq.Live, migReq.TimeoutSeconds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("VM migrated successfully"))
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
	vm, err := virshServices.GetVmByName(vmName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vm == nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}

	if vm.State != grpcVirsh.VmState_SHUTOFF {
		http.Error(w, "VM must be shut off to export", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+vmName+".qcow2\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, vm.DiskPath)
}

const bufSize = 8 << 20 // 8 MiB
func q(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

type VMRequest struct {
	Slave_name  string `json:"slave_name"`
	NfsShareId  int    `json:"nfs_share_id"`
	VmName      string `json:"vm_name"`
	Memory      int32  `json:"memory"`
	Vcpu        int32  `json:"vcpu"`
	Network     string `json:"network"`
	VNCPassword string `json:"VNC_password"`
	CpuXML      string `json:"cpu_xml"`
}

func readVMRequest(r *http.Request) (*VMRequest, error) {
	var vmReq VMRequest
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

	//chmod 777 file
	_ = os.Chmod(finalFile, 0o777)

	err = virshService.ColdMigrateVm(
		r.Context(),
		vmReq.Slave_name,
		&grpcVirsh.ColdMigrationRequest{
			VmName:      vmReq.VmName,
			DiskPath:    finalFile,
			Memory:      vmReq.Memory,
			VCpus:       vmReq.Vcpu,
			Network:     vmReq.Network,
			VncPassword: vmReq.VNCPassword,
			CpuXML:      vmReq.CpuXML,
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

	virshServices := services.VirshService{}
	err = virshServices.BackupVM(vmName, nfsIdInt)
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

	w.Header().Set("Content-Disposition", "attachment; filename=\""+bak.Name+".qcow2\"")
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

	var vmReq VMRequest
	err = json.NewDecoder(r.Body).Decode(&vmReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	virshServices := services.VirshService{}
	err = virshServices.UseBackup(r.Context(), backupIdInt,
		vmReq.Slave_name, vmReq.NfsShareId,
		&grpcVirsh.ColdMigrationRequest{
			VmName:      vmReq.VmName,
			Memory:      vmReq.Memory,
			VCpus:       vmReq.Vcpu,
			Network:     vmReq.Network,
			VncPassword: vmReq.VNCPassword,
			CpuXML:      vmReq.CpuXML,
			DiskPath:    "", //UseBackup FUNCTION WILL SET THIS
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
	_ = backupIdInt

	w.WriteHeader(http.StatusOK)
}

func setupVirshAPI(r chi.Router) chi.Router {
	return r.Route("/virsh", func(r chi.Router) {
		r.Get("/getcpudisablefeatures", getCpuFeatures)
		r.Get("/getallvms", getAllVms)
		r.Post("/createvm", createVM)
		r.Post("/createlivevm", createLiveVM)

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

		//export/import
		r.Get("/export/{vm_name}", exportVM)
		r.Put("/import", importVM)

		//backups
		r.Post("/backup/{vm_name}/{nfs_id}", backupVM)
		r.Get("/backups", getAllBackups)
		r.Get("/downloadbackup/{backup_id}", downloadBackup)
		r.Post("/useBackup/{backup_id}", useBackup)
		r.Delete("/delete/{backup_id}", deleteBackup)
	})
}
