package api

import (
	"512SvMan/services"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// number of logs, importance of logs
func listLogs(w http.ResponseWriter, r *http.Request) {
	//get number of logs from query param
	limit := r.URL.Query().Get("limit")
	level := r.URL.Query().Get("level")

	//convert to int
	if limit == "" {
		limit = "100"
	}
	if level == "" {
		level = "0"
	}

	//convert to int
	limitInt := 100
	levelInt := 0
	_, err := fmt.Sscanf(limit, "%d", &limitInt)
	if err != nil {
		http.Error(w, "Invalid limit", http.StatusBadRequest)
		return
	}
	_, err = fmt.Sscanf(level, "%d", &levelInt)
	if err != nil {
		http.Error(w, "Invalid level", http.StatusBadRequest)
		return
	}

	logsService := services.LogsService{}
	logs, err := logsService.GetLogs(r.Context(), limitInt, levelInt)
	if err != nil {
		http.Error(w, "Failed to get logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

func setupLogsAPI(r chi.Router) chi.Router {
	return r.Route("/logs", func(r chi.Router) {
		r.Get("/list", listLogs)
	})
}
