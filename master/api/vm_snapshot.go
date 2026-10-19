package api

import (
	"512SvMan/services"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
)

func getVMSnapshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	name := strings.TrimSpace(chi.URLParam(r, "vm_name"))
	if name == "" {
		http.Error(w, "vm_name is required", 400)
		return
	}
	machine := strings.TrimSpace(r.URL.Query().Get("machine_name"))
	svc := services.VirshService{}
	if machine == "" {
		vm, err := svc.GetVmByName(name)
		if err != nil {
			http.Error(w, "VM identity unavailable", 500)
			return
		}
		if vm == nil {
			http.Error(w, "VM not found", 404)
			return
		}
		machine = vm.MachineName
	}
	snapshot, err := svc.GetVMSnapshot(r.Context(), machine, name)
	if err != nil {
		http.Error(w, "secure VM snapshot unavailable", 500)
		return
	}
	// Explicit tags emit inactive/current XML and false active values unchanged.
	result := struct {
		Name        string `json:"name"`
		UUID        string `json:"uuid"`
		MachineName string `json:"machine_name"`
		Active      bool   `json:"active"`
		InactiveXML string `json:"inactive_xml"`
		CurrentXML  string `json:"current_xml"`
	}{snapshot.Name, snapshot.Uuid, machine, snapshot.Active, snapshot.InactiveXml, snapshot.CurrentXml}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
