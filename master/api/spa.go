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
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SPA Allow Port %s</title>
  <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="min-h-screen bg-black text-zinc-50 flex items-center justify-center p-4">
  <div class="w-full max-w-md rounded-2xl border border-zinc-800 bg-zinc-950/90 shadow-2xl shadow-black/70 px-5 py-6">
    <div class="flex items-center gap-3 mb-4">
      <div class="w-10 h-10 rounded-full border border-zinc-700 bg-black/40 flex items-center justify-center overflow-hidden">
        <img src="/api/icon.png" alt="Logo" class="w-7 h-7">
      </div>
      <div class="flex-1 min-w-0">
        <h2 class="text-sm font-semibold tracking-wide text-zinc-50">
          Authorize Access on Port
        </h2>
        <div class="mt-2 inline-flex items-center gap-2 rounded-full border border-zinc-700 px-3 py-1">
          <span class="h-1.5 w-1.5 rounded-full bg-zinc-50"></span>
          <span class="text-[11px] font-medium uppercase tracking-[0.2em] text-zinc-200">
            Port %s
          </span>
        </div>
      </div>
    </div>

    <p class="text-sm text-zinc-300 mb-4 leading-relaxed">
      Enter the SPA password and for how many seconds this IP should be allowed to access this port.
    </p>

    <form id="allow-form" class="space-y-3">
      <label class="block text-xs font-medium text-zinc-100">
        Password
        <input
          type="password"
          id="password"
          autocomplete="current-password"
          required
          class="mt-1 block w-full rounded-lg border border-zinc-700 bg-black/40 px-3 py-2 text-sm text-zinc-50 placeholder-zinc-500 outline-none focus:ring-1 focus:ring-zinc-100 focus:border-zinc-100"
        >
      </label>

      <div>
        <label class="block text-xs font-medium text-zinc-100">
          Seconds
          <input
            type="number"
            id="seconds"
            value="28800"
            min="1"
            required
            class="mt-1 block w-full rounded-lg border border-zinc-700 bg-black/40 px-3 py-2 text-sm text-zinc-50 placeholder-zinc-500 outline-none focus:ring-1 focus:ring-zinc-100 focus:border-zinc-100"
          >
        </label>

        <!-- Quick presets -->
        <div class="mt-2 flex flex-wrap gap-2">
          <button type="button" data-seconds="3600"
            class="px-3 py-1.5 rounded-full border border-zinc-700 text-[11px] font-medium text-zinc-200 hover:border-zinc-200 hover:text-zinc-50 transition-colors">
            1h
          </button>
          <button type="button" data-seconds="7200"
            class="px-3 py-1.5 rounded-full border border-zinc-700 text-[11px] font-medium text-zinc-200 hover:border-zinc-200 hover:text-zinc-50 transition-colors">
            2h
          </button>
          <button type="button" data-seconds="14400"
            class="px-3 py-1.5 rounded-full border border-zinc-700 text-[11px] font-medium text-zinc-200 hover:border-zinc-200 hover:text-zinc-50 transition-colors">
            4h
          </button>
          <button type="button" data-seconds="28800"
            class="px-3 py-1.5 rounded-full border border-zinc-700 text-[11px] font-medium text-zinc-200 hover:border-zinc-200 hover:text-zinc-50 transition-colors">
            8h
          </button>
          <button type="button" data-seconds="86400"
            class="px-3 py-1.5 rounded-full border border-zinc-700 text-[11px] font-medium text-zinc-200 hover:border-zinc-200 hover:text-zinc-50 transition-colors">
            24h
          </button>
        </div>
      </div>

      <button
        type="submit"
        class="mt-2 inline-flex w-full items-center justify-center rounded-full border border-zinc-50 bg-zinc-50 px-3 py-2.5 text-sm font-semibold text-black shadow-lg shadow-zinc-950/50 transition-transform disabled:opacity-60 disabled:cursor-not-allowed hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-zinc-100"
      >
        Allow my IP
      </button>
    </form>

    <div
      id="msg"
      class="mt-4 min-h-[2.5rem] rounded-lg border border-zinc-800 bg-black/40 px-3 py-2 text-[11px] font-mono text-zinc-100 whitespace-pre-wrap"
    ></div>
  </div>

  <script>
    const form = document.getElementById('allow-form');
    const msg = document.getElementById('msg');
    const secondsInput = document.getElementById('seconds');
    const presetButtons = document.querySelectorAll('[data-seconds]');

    // Botões de preset (1h, 2h, ...)
    presetButtons.forEach((btn) => {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        const secs = btn.getAttribute('data-seconds');
        if (secs) {
          secondsInput.value = secs;
        }
      });
    });

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      msg.textContent = '';
      const password = document.getElementById('password').value;
      const seconds = parseInt(secondsInput.value, 10) || 0;

      if (!password || seconds <= 0) {
        msg.textContent = 'Password and positive seconds are required.';
        return;
      }

      const payload = { port: %s, password, seconds };
      const submitBtn = form.querySelector('button[type="submit"]');
      submitBtn.disabled = true;

      try {
        const res = await fetch('/api/spa/allow', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const text = await res.text();
        if (!res.ok) {
          msg.textContent = 'Error ' + res.status + ': ' + text;
        } else {
          msg.textContent = 'Success: ' + text;
        }
      } catch (err) {
        msg.textContent = 'Request failed: ' + err;
      } finally {
        submitBtn.disabled = false;
      }
    });
  </script>
</body>
</html>`, portStr, portStr, portStr)

	_, _ = w.Write([]byte(page))
}

func setupSPAOpenAPI(r chi.Router) {
	r.Post("/spa/allow", allowSPAHandler)
	r.Get("/spa/pageallow/{port}", serveSPAPageAllow)

}

func setupSPAAPI(r chi.Router) {
	r.Post("/spa", createSPAHandler)
	r.Get("/spa", listSPAHandler)
	r.Delete("/spa/{port}", deleteSPAHandler)
}
