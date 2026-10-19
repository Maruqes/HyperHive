package api

import (
	"512SvMan/services"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"
)

func getHostBootSnapshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	machine := strings.TrimSpace(chi.URLParam(r, "machine_name"))
	if machine == "" {
		http.Error(w, "machine_name is required", 400)
		return
	}
	svc := services.VirshService{}
	snapshot, err := svc.GetHostBootSnapshot(r.Context(), machine)
	if err != nil {
		http.Error(w, "host snapshot unavailable", 500)
		return
	}
	type bootEntry struct {
		Index  int32  `json:"index"`
		Kernel string `json:"kernel"`
		Args   string `json:"args"`
	}
	entries := make([]bootEntry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if entry == nil {
			http.Error(w, "host snapshot unavailable", 500)
			return
		}
		entries = append(entries, bootEntry{entry.Index, entry.Kernel, entry.Args})
	}
	result := struct {
		MachineName    string      `json:"machine_name"`
		GrubbyInfo     string      `json:"grubby_info"`
		CurrentCmdline string      `json:"current_cmdline"`
		DefaultKernel  string      `json:"default_kernel"`
		Entries        []bootEntry `json:"entries"`
	}{machine, snapshot.GrubbyInfo, snapshot.CurrentCmdline, snapshot.DefaultKernel, entries}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
