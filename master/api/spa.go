package api

import (
	"512SvMan/services"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func createSPAHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Port     int    `json:"port"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Port == 0 || req.Password == "" {
		http.Error(w, "port and password are required", http.StatusBadRequest)
		return
	}

	svc := services.SPAService{}
	if err := svc.Create(r.Context(), req.Port, req.Password); err != nil {
		writeSPAError(w, err)
		return
	}

	writeJSONWithStatus(w, http.StatusCreated, map[string]any{
		"status": "created",
		"port":   req.Port,
	})
}

func deleteSPAHandler(w http.ResponseWriter, r *http.Request) {
	portStr := chi.URLParam(r, "port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}

	svc := services.SPAService{}
	if err := svc.Delete(r.Context(), port); err != nil {
		writeSPAError(w, err)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"status": "deleted",
		"port":   port,
	})
}

func allowSPAHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Port     int    `json:"port"`
		Password string `json:"password"`
		Seconds  int    `json:"seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Port == 0 || req.Password == "" {
		http.Error(w, "port and password are required", http.StatusBadRequest)
		return
	}
	if req.Seconds <= 0 {
		http.Error(w, "seconds must be positive", http.StatusBadRequest)
		return
	}

	ip, err := clientIP(r)
	if err != nil {
		http.Error(w, "could not determine client IP", http.StatusBadRequest)
		return
	}

	if net.ParseIP(ip) == nil {
		http.Error(w, "invalid IP address", http.StatusBadRequest)
		return
	}

	svc := services.SPAService{}
	if err := svc.Allow(r.Context(), req.Port, req.Password, ip, req.Seconds); err != nil {
		writeSPAError(w, err)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"status":  "allowed",
		"port":    req.Port,
		"ip":      ip,
		"seconds": req.Seconds,
	})
}

func writeSPAError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrSPAPortNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, services.ErrInvalidPassword):
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func writeJSONWithStatus(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func clientIP(r *http.Request) (string, error) {
	// Prefer X-Forwarded-For, then X-Real-Ip, then remote addr.
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		for _, part := range strings.Split(xf, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" && net.ParseIP(trimmed) != nil {
				return trimmed, nil
			}
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" && net.ParseIP(xr) != nil {
		return xr, nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host, nil
	}
	return "", fmt.Errorf("no valid client ip")
}

func setupSPAOpenAPI(r chi.Router) {
	r.Post("/spa/allow", allowSPAHandler)
}

func setupSPAAPI(r chi.Router) {
	r.Post("/spa", createSPAHandler)
	r.Delete("/spa/{port}", deleteSPAHandler)
}
