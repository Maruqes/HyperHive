package api

import (
	"512SvMan/dnsmasq"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type dnsAliasRequest struct {
	Alias string `json:"alias"`
	IP    string `json:"ip"`
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

	if err := dnsmasq.AddAlias(req.Alias, req.IP); err != nil {
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
		r.Get("/alias/get", getDNSAlias)
		r.Post("/alias/add", addDNSAlias)
		r.Delete("/alias/remove", removeDNSAlias)
	})
}
