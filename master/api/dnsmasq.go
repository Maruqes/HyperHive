package api

import (
	"512SvMan/dnsmasq"
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type dnsAliasRequest struct {
	Alias       string `json:"alias"`
	IP          string `json:"ip"`
	SSHUsername string `json:"ssh_username"`
	RelatedVM   string `json:"related_vm"`
}

var (
	dnsAliasList = dnsmasq.GetAllAliases
	dnsAliasAdd  = dnsmasq.AddAlias
	dnsAliasVMs  = (&services.VirshService{}).GetAllVms
)

func getAllDNSAliases(w http.ResponseWriter, r *http.Request) {
	entries, err := dnsAliasList()
	if err != nil {
		http.Error(w, "failed to get aliases: "+err.Error(), http.StatusInternalServerError)
		return
	}

	aliases := make([]dnsAliasRequest, len(entries))
	for i, entry := range entries {
		aliases[i] = dnsAliasRequest{
			Alias:       entry.Alias,
			IP:          entry.IP,
			SSHUsername: entry.SSHUsername,
			RelatedVM:   entry.RelatedVM,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string][]dnsAliasRequest{
		"aliases": aliases,
	})
}

func getDNSAlias(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if alias == "" || ip == "" {
		http.Error(w, "alias and ip are required query params", http.StatusBadRequest)
		return
	}

	exists, err := dnsmasq.GetAlias(alias, ip)
	if err != nil {
		http.Error(w, "failed to get alias: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"exists": exists,
	})
}

func addDNSAlias(w http.ResponseWriter, r *http.Request) {
	var req dnsAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.RelatedVM != "" {
		vms, _, err := dnsAliasVMs(r.Context())
		if err != nil {
			http.Error(w, "failed to validate related VM: "+err.Error(), http.StatusInternalServerError)
			return
		}
		found := false
		for _, vm := range vms {
			if vm.Vm != nil && vm.Name == req.RelatedVM {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "related_vm must name an existing VM", http.StatusBadRequest)
			return
		}
	}

	if err := dnsAliasAdd(req.Alias, req.IP, req.SSHUsername, req.RelatedVM); err != nil {
		http.Error(w, "failed to add alias: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func removeDNSAlias(w http.ResponseWriter, r *http.Request) {
	var req dnsAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := dnsmasq.RemoveAlias(req.Alias, req.IP); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "alias not found") {
			status = http.StatusNotFound
		}
		http.Error(w, "failed to remove alias: "+err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func setupDNSMasqAPI(r chi.Router) chi.Router {
	return r.Route("/dnsmasq", func(r chi.Router) {
		r.Get("/alias/all", getAllDNSAliases)
		r.Get("/alias/get", getDNSAlias)
		r.Post("/alias/add", addDNSAlias)
		r.Delete("/alias/remove", removeDNSAlias)
	})
}
