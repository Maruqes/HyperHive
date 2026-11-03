package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

func diskHealthSerialParam(r *http.Request) string {
	raw := chi.URLParam(r, "serial")
	if serial, err := url.PathUnescape(raw); err == nil {
		return strings.TrimSpace(serial)
	}
	return strings.TrimSpace(raw)
}

func writeDiskHealthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrDiskHealthDisabled):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	case errors.Is(err, services.ErrInvalidDiskHealthQuery):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, db.ErrDiskNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, db.ErrDiskNotRetired):
		http.Error(w, "only retired disks can be purged", http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// listDiskHealth returns disks with their score, band and latest sample.
func listDiskHealth(w http.ResponseWriter, r *http.Request) {
	service := services.DiskHealthService{}
	list, err := service.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("state")))
	if err != nil {
		writeDiskHealthError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, list)
}

// getDiskHealthHistory returns one disk and its chart points; ?days=N limits the range.
func getDiskHealthHistory(w http.ResponseWriter, r *http.Request) {
	days := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			http.Error(w, "days must be a positive integer", http.StatusBadRequest)
			return
		}
		days = n
	}
	service := services.DiskHealthService{}
	history, err := service.History(r.Context(), diskHealthSerialParam(r), days)
	if err != nil {
		writeDiskHealthError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, history)
}

// purgeDiskHealth deletes a retired disk and its history. It is UI-only and
// never exposed through MCP.
func purgeDiskHealth(w http.ResponseWriter, r *http.Request) {
	service := services.DiskHealthService{}
	if err := service.Purge(r.Context(), diskHealthSerialParam(r)); err != nil {
		writeDiskHealthError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"status": "purged"})
}

func setupDiskHealthAPI(r chi.Router) chi.Router {
	return r.Route("/diskhealth", func(r chi.Router) {
		r.Get("/", listDiskHealth)
		r.Get("/{serial}", getDiskHealthHistory)
		r.Delete("/{serial}", purgeDiskHealth)
	})
}
