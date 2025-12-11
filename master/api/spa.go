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
	"time"

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

func listSPAHandler(w http.ResponseWriter, r *http.Request) {
	svc := services.SPAService{}
	entries, err := svc.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type spaOut struct {
		Port      int       `json:"port"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]spaOut, 0, len(entries))
	for _, e := range entries {
		out = append(out, spaOut{
			Port:      e.Port,
			CreatedAt: e.CreatedAt,
		})
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"spa_ports": out,
	})
}

func serveSPAPageAllow(w http.ResponseWriter, r *http.Request) {
	portStr := chi.URLParam(r, "port")
	if portStr == "" {
		http.Error(w, "missing port", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SPA Allow Port %s</title>
<style>
body { font-family: Arial, sans-serif; max-width: 480px; margin: 40px auto; padding: 0 16px; color: #0f172a; }
form { display: flex; flex-direction: column; gap: 12px; margin-top: 16px; }
input, button { padding: 10px 12px; font-size: 16px; }
button { background: #0f172a; color: #fff; border: none; cursor: pointer; }
button:disabled { opacity: 0.6; cursor: not-allowed; }
.msg { margin-top: 12px; font-family: monospace; white-space: pre-wrap; }
</style>
</head>
<body>
<h2>Authorize Access on Port %s</h2>
<p>Enter the SPA password and how many seconds this IP should be allowed.</p>
<form id="allow-form">
  <label>Password<br><input type="password" id="password" required></label>
  <label>Seconds<br><input type="number" id="seconds" value="28800" min="1" required></label>
  <button type="submit">Allow my IP</button>
</form>
<div class="msg" id="msg"></div>
<script>
const form = document.getElementById('allow-form');
const msg = document.getElementById('msg');

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  msg.textContent = '';
  const password = document.getElementById('password').value;
  const seconds = parseInt(document.getElementById('seconds').value, 10) || 0;
  if (!password || seconds <= 0) {
    msg.textContent = 'Password and positive seconds are required.';
    return;
  }
  const payload = { port: %s, password, seconds };
  try {
    const res = await fetch('/spa/allow', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const text = await res.text();
    if (!res.ok) {
      msg.textContent = 'Error ' + res.status + ': ' + text;
    } else {
      msg.textContent = 'Success: ' + text;
    }
  } catch (err) {
    msg.textContent = 'Request failed: ' + err;
  }
});
</script>
</body>
</html>`, portStr, portStr, portStr)

	_, _ = w.Write([]byte(page))
}

func setupSPAOpenAPI(r chi.Router) {
	r.Post("/spa/allow", allowSPAHandler)
}

func setupSPAAPI(r chi.Router) {
	r.Post("/spa", createSPAHandler)
	r.Get("/spa", listSPAHandler)
	r.Delete("/spa/{port}", deleteSPAHandler)
}

func setupSPAPageAPI(r chi.Router) chi.Router {
	return r.Route("/spa", func(r chi.Router) {
		r.Get("/pageallow/{port}", serveSPAPageAllow)
	})
}
