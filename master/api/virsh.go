package api

import (
	"512SvMan/services"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

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

	w.Header().Set("Content-Disposition", "attachment; filename=\""+vmName+".qcow2\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, vm.DiskPath)
}

const bufSize = 8 << 20 // 8 MiB

func importVM(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" || vmName == "." || vmName == ".." {
		http.Error(w, "invalid vmname "+vmName, http.StatusBadRequest)
		return
	}
	nfsID := chi.URLParam(r, "nfs_id")
	if nfsID == "" {
		http.Error(w, "nfs_id is required", http.StatusBadRequest)
		return
	}
	if r.ContentLength <= 0 {
		http.Error(w, "Content-Length required", http.StatusLengthRequired)
		return
	}

	virshService := services.VirshService{}
	nid, err := strconv.Atoi(nfsID)
	if err != nil {
		http.Error(w, "invalid nfs_id", http.StatusBadRequest)
		return
	}

	finalFile, err := virshService.ImportVmHelper(nid, vmName)
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
	w.WriteHeader(http.StatusCreated)
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
		r.Put("/import/{vm_name}/{nfs_id}", importVM)
	})
}
