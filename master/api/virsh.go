package api

import (
	"512SvMan/protocol"
	"512SvMan/services"
	"512SvMan/virsh"
	"encoding/json"
	"net/http"

	proto "github.com/Maruqes/512SvMan/api/proto/virsh"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/go-chi/chi/v5"
)

func getCpuFeatures(w http.ResponseWriter, r *http.Request) {
	virshServices := services.VirshService{}
	w.Header().Set("Content-Type", "application/json")
	features, err := virshServices.GetCpuDisableFeatures()
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

	var res []*proto.GetAllVmsResponse
	cons := protocol.GetAllGRPCConnections()
	for _, conn := range cons {
		vms, err := virsh.GetAllVms(conn, &grpcVirsh.Empty{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res = append(res, vms)
	}
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func setupVirshAPI(r chi.Router) chi.Router {
	return r.Route("/virsh", func(r chi.Router) {
		r.Get("/getcpudisablefeatures", getCpuFeatures)
		r.Post("/createvm", createVM)
		r.Get("/getallvms", getAllVms)
	})
}
