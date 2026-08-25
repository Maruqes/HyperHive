package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GUESTS.go aggregates public-facing endpoints served under /guest_api.
// Currently this includes guest noVNC access and the public SPA (Single Packet
// Authorization) allow/page endpoints. SPA management routes live in /spa.

// -----------------------------------------------------------------------------
// Guest token store
// -----------------------------------------------------------------------------

type guestTokenInfo struct {
	VMName    string
	ExpiresAt time.Time
}

type guestTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]guestTokenInfo
}

func newGuestTokenStore() *guestTokenStore {
	return &guestTokenStore{
		tokens: make(map[string]guestTokenInfo),
	}
}

func (s *guestTokenStore) set(token string, vmName string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = guestTokenInfo{
		VMName:    vmName,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (s *guestTokenStore) get(token string) (guestTokenInfo, bool) {
	s.mu.RLock()
	info, ok := s.tokens[token]
	s.mu.RUnlock()
	if !ok {
		return guestTokenInfo{}, false
	}

	// expire eagerly to avoid long-lived access
	if time.Now().After(info.ExpiresAt) {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return guestTokenInfo{}, false
	}

	return info, true
}

var guestTokens = newGuestTokenStore()

const guestTokenTTL = 24 * time.Hour

// -----------------------------------------------------------------------------
// Guest password rate limiter
// -----------------------------------------------------------------------------

const (
	guestRateLimitAttempts = 5
	guestRateLimitWindow   = 15 * time.Minute
	guestAuthErrorMessage  = "invalid credentials"
)

type guestRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*guestAttempt
}

type guestAttempt struct {
	count int
	reset time.Time
}

func newGuestRateLimiter() *guestRateLimiter {
	return &guestRateLimiter{
		attempts: make(map[string]*guestAttempt),
	}
}

func (rl *guestRateLimiter) allow(key string) (allowed bool, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	att, ok := rl.attempts[key]
	if !ok || now.After(att.reset) {
		return true, 0
	}
	if att.count >= guestRateLimitAttempts {
		return false, time.Until(att.reset)
	}
	return true, 0
}

func (rl *guestRateLimiter) recordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	att, ok := rl.attempts[key]
	if !ok || now.After(att.reset) {
		rl.attempts[key] = &guestAttempt{
			count: 1,
			reset: now.Add(guestRateLimitWindow),
		}
		return
	}
	att.count++
}

func (rl *guestRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, att := range rl.attempts {
		if now.After(att.reset) {
			delete(rl.attempts, key)
		}
	}
}

var guestRateLimiterInstance = newGuestRateLimiter()

func init() {
	go func() {
		for {
			time.Sleep(guestRateLimitWindow)
			guestRateLimiterInstance.cleanup()
		}
	}()
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// -----------------------------------------------------------------------------
// Guest VM password page
// -----------------------------------------------------------------------------

func serveGuestPage(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>noVNC – %s</title>
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
          Connect to Virtual Machine
        </h2>
        <div class="mt-2 inline-flex items-center gap-2 rounded-full border border-zinc-700 px-3 py-1">
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
          <span class="text-[11px] font-medium uppercase tracking-[0.2em] text-zinc-200">
            VM %s
          </span>
        </div>
      </div>
    </div>

    <p class="text-sm text-zinc-300 mb-4 leading-relaxed">
      Enter the access password to open a secure noVNC session for this VM.
      After a successful check you'll be automatically redirected to the console.
    </p>

    <form id="guest-form" class="space-y-3">
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

      <button
        type="submit"
        class="mt-2 inline-flex w-full items-center justify-center rounded-full border border-zinc-50 bg-zinc-50 px-3 py-2.5 text-sm font-semibold text-black shadow-lg shadow-zinc-950/50 transition-transform disabled:opacity-60 disabled:cursor-not-allowed hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-zinc-100"
      >
        Connect to noVNC
      </button>
    </form>

    <div
      id="msg"
      class="mt-4 min-h-[2.5rem] rounded-lg border border-zinc-800 bg-black/40 px-3 py-2 text-[11px] font-mono text-zinc-100 whitespace-pre-wrap"
    ></div>
  </div>

  <script>
    const form = document.getElementById('guest-form');
    const msg = document.getElementById('msg');
    const passwordInput = document.getElementById('password');

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      msg.textContent = '';

      const password = passwordInput.value.trim();
      if (!password) {
        msg.textContent = 'Password is required.';
        return;
      }

      const submitBtn = form.querySelector('button[type="submit"]');
      submitBtn.disabled = true;

      try {
        const res = await fetch('/guest_api/guest_vm', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({ vm_name: '%s', password: password })
        });

        if (!res.ok) {
          const text = await res.text();
          msg.textContent = 'Error ' + res.status + ': ' + text;
        } else {
          // Password OK -> redirecionar para noVNC
		  window.location.href = '/guest_api/novnc/vnc.html?path=/guest_api/novnc/ws%%3Fvm%%3D%s';
        }
      } catch (err) {
        msg.textContent = 'Request failed: ' + err;
      } finally {
        submitBtn.disabled = false;
      }
    });
  </script>
</body>
</html>`, vmName, vmName, vmName, vmName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func guestPost(w http.ResponseWriter, r *http.Request) {
	clientIP := getClientIP(r)
	if allowed, retryAfter := guestRateLimiterInstance.allow(clientIP); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req struct {
		VMName   string `json:"vm_name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	virshService := services.VirshService{}
	vm, err := virshService.GetVmByName(req.VMName)
	if err != nil || vm == nil {
		guestRateLimiterInstance.recordFailure(clientIP)
		http.Error(w, guestAuthErrorMessage, http.StatusUnauthorized)
		return
	}

	if req.Password == "" || req.Password != vm.VNCPassword {
		guestRateLimiterInstance.recordFailure(clientIP)
		http.Error(w, guestAuthErrorMessage, http.StatusUnauthorized)
		return
	}

	token := uuid.New().String()
	guestTokens.set(token, req.VMName, guestTokenTTL)

	http.SetCookie(w, &http.Cookie{
		Name:     "guest_token",
		Value:    token,
		Path:     "/guest_api/novnc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(guestTokenTTL.Seconds()),
		Expires:  time.Now().Add(guestTokenTTL),
	})

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Password processed"))
}

// -----------------------------------------------------------------------------
// Guest noVNC middleware and helpers
// -----------------------------------------------------------------------------

type novncCtxKey string

const guestVMContextKey novncCtxKey = "guest_vm"

func guestVMFromContext(r *http.Request) string {
	if vm, ok := r.Context().Value(guestVMContextKey).(string); ok {
		return vm
	}
	return ""
}

func extractVMFromRequest(r *http.Request) string {
	if vm := r.URL.Query().Get("vm"); vm != "" {
		return vm
	}
	if vm := chi.URLParam(r, "vm_name"); vm != "" {
		return vm
	}

	// Attempt to pull vm from encoded path query param e.g. path=/guest_api/novnc/ws%3Fvm%3D<vm_name>
	if rawPath := r.URL.Query().Get("path"); rawPath != "" {
		if decoded, err := url.QueryUnescape(rawPath); err == nil {
			if u, err := url.Parse(decoded); err == nil {
				if vm := u.Query().Get("vm"); vm != "" {
					return vm
				}
			}
		}
	}

	return ""
}

func checkGuestToken(r *http.Request, requestedVM string) (bool, string) {
	cookie, err := r.Cookie("guest_token")
	if err != nil {
		return false, ""
	}

	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return false, ""
	}

	info, exists := guestTokens.get(token)
	if !exists {
		return false, ""
	}

	if requestedVM != "" && info.VMName != requestedVM {
		logger.Warnf("novnc: guest token for vm %s attempted to access vm %s", info.VMName, requestedVM)
		return false, ""
	}

	return true, info.VMName
}

func guestAuthMiddlewareNOVNC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedVM := extractVMFromRequest(r)

		authorized, vm := checkGuestToken(r, requestedVM)
		if authorized {
			if vm != "" {
				r = r.WithContext(context.WithValue(r.Context(), guestVMContextKey, vm))
			}
			next.ServeHTTP(w, r)
			return
		}

		applyCORSHeaders(w, r)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func serveGuestNoVNC(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/guest_api/novnc", http.FileServer(http.Dir("./novnc"))).ServeHTTP(w, r)
}

// -----------------------------------------------------------------------------
// SPA handlers
// -----------------------------------------------------------------------------

const spaPortResourceType = "spa_port"

func writeSPAError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrSPAPortNotFound), errors.Is(err, services.ErrInvalidPassword):
		http.Error(w, guestAuthErrorMessage, http.StatusUnauthorized)
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
			if trimmed == "" {
				continue
			}
			if ip := net.ParseIP(trimmed); ip != nil && ip.To4() != nil {
				return ip.String(), nil
			}
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		if ip := net.ParseIP(xr); ip != nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no valid client ip")
}

func createSPAHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Port        int    `json:"port"`
		Password    string `json:"password"`
		Description string `json:"description"`
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
	if err := db.SetResourceDescription(r.Context(), spaPortResourceType, req.Port, req.Description); err != nil {
		http.Error(w, "failed to save SPA port description: "+err.Error(), http.StatusInternalServerError)
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
	if err := db.DeleteResourceDescription(r.Context(), spaPortResourceType, port); err != nil {
		http.Error(w, "failed to delete SPA port description: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"status": "deleted",
		"port":   port,
	})
}

func allowSPAHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	callerIP, err := clientIP(r)
	if err != nil {
		http.Error(w, "could not determine client IP; provide \"ip\" in the request body", http.StatusBadRequest)
		return
	}
	if allowed, retryAfter := guestRateLimiterInstance.allow(callerIP); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Port     int    `json:"port"`
		Password string `json:"password"`
		Seconds  int    `json:"seconds"`
		IP       string `json:"ip"`
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

	manualIP := strings.TrimSpace(req.IP)
	var ip string
	if manualIP != "" {
		parsed := net.ParseIP(manualIP)
		if parsed == nil || parsed.To4() == nil {
			http.Error(w, "invalid IPv4 address", http.StatusBadRequest)
			return
		}
		ip = parsed.String()
	}

	svc := services.SPAService{}
	if err := svc.Allow(r.Context(), req.Port, req.Password, ip, req.Seconds); err != nil {
		guestRateLimiterInstance.recordFailure(callerIP)
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

func listSPAHandler(w http.ResponseWriter, r *http.Request) {
	svc := services.SPAService{}
	entries, err := svc.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type spaOut struct {
		Port        int       `json:"port"`
		Description string    `json:"description"`
		CreatedAt   time.Time `json:"created_at"`
	}
	ports := make([]int, 0, len(entries))
	for _, entry := range entries {
		ports = append(ports, entry.Port)
	}
	descriptions, err := db.GetResourceDescriptions(r.Context(), spaPortResourceType, ports)
	if err != nil {
		http.Error(w, "failed to get SPA port descriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]spaOut, 0, len(entries))
	for _, e := range entries {
		out = append(out, spaOut{
			Port:        e.Port,
			Description: descriptions[e.Port],
			CreatedAt:   e.CreatedAt,
		})
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"spa_ports": out,
	})
}

func listSPAAllowsHandler(w http.ResponseWriter, r *http.Request) {
	portStr := chi.URLParam(r, "port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}

	svc := services.SPAService{}
	entries, err := svc.ListAllows(r.Context(), port)
	if err != nil {
		writeSPAError(w, err)
		return
	}

	type allowOut struct {
		IP               string `json:"ip"`
		RemainingSeconds int    `json:"remaining_seconds"`
	}
	out := make([]allowOut, 0, len(entries))
	for _, entry := range entries {
		out = append(out, allowOut{
			IP:               entry.IP,
			RemainingSeconds: entry.RemainingSeconds,
		})
	}

	writeJSONWithStatus(w, http.StatusOK, map[string]any{
		"port":   port,
		"allows": out,
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
      Enter the SPA password and for how many seconds this IP should be allowed to access this port. You can set the IP manually, or leave it blank to auto-detect from your request.
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

      <label class="block text-xs font-medium text-zinc-100">
        IP (optional)
        <input
          type="text"
          id="ip"
          inputmode="numeric"
          placeholder="Leave blank to auto-detect"
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
    const ipInput = document.getElementById('ip');
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
      const ip = ipInput.value.trim();

      if (!password || seconds <= 0) {
        msg.textContent = 'Password and positive seconds are required.';
        return;
      }

      const payload = { port: %s, password, seconds };
      if (ip) {
        payload.ip = ip;
      }
      const submitBtn = form.querySelector('button[type="submit"]');
      submitBtn.disabled = true;

      try {
        const res = await fetch('/guest_api/spa/allow-ip', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const text = await res.text();
        if (!res.ok) {
          msg.textContent = 'Error ' + res.status + ': ' + text;
        } else {
          try {
            const data = JSON.parse(text);
            msg.textContent = 'Success: IP ' + data.ip + ' allowed for ' + data.seconds + ' seconds on port ' + data.port;
          } catch (_) {
            msg.textContent = 'Success: ' + text;
          }
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

// -----------------------------------------------------------------------------
// Guest API route setup
// -----------------------------------------------------------------------------

func setupGuestPublicAPI(r chi.Router) {
	r.Route("/guest_api", func(r chi.Router) {
		r.Get("/guest_page/{vm_name}", serveGuestPage)
		r.Post("/guest_vm", guestPost)

		//guest novnc
		r.Route("/novnc", func(r chi.Router) {
			r.Use(guestAuthMiddlewareNOVNC)

			r.Get("/ws", serveNoVNCWebSocket)
			r.Get("/sprite/{vm_name}", serveSprite)
			r.Get("/*", serveGuestNoVNC)
		})

		r.Route("/spa", func(r chi.Router) {
			r.Post("/allow-ip", allowSPAHandler)
			r.Post("/allow", allowSPAHandler) // backward compatible
			r.Get("/pageallow/{port}", serveSPAPageAllow)
		})
	})
}

func setupSPAAPI(r chi.Router) {
	r.Route("/spa", func(r chi.Router) {
		r.Post("/", createSPAHandler)
		r.Get("/", listSPAHandler)
		r.Get("/allow/{port}", listSPAAllowsHandler)
		r.Delete("/{port}", deleteSPAHandler)
	})
}
