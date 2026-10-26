package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type supportProxy struct {
	handler http.Handler
	mu      sync.Mutex
	tickets map[string]time.Time
	origins map[string]bool
}

func supportError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func newSupportProxy(address, secret, allowedOrigins string) *supportProxy {
	service := &supportProxy{tickets: make(map[string]time.Time), origins: make(map[string]bool)}
	for _, origin := range strings.Split(allowedOrigins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			service.origins[origin] = true
		}
	}
	target, err := url.Parse(strings.TrimSpace(address))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Path != "" && target.Path != "/") || len(secret) < 32 {
		service.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			supportError(w, http.StatusServiceUnavailable, "HH Support requires SUPPORT_URL and a SUPPORT_PROXY_SECRET of at least 32 characters")
		})
		return service
	}
	target.Path = ""
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Header.Del("X-HH-Support-Key")
			request.Out.Header.Set("X-HH-Support-Key", secret)
			request.Out.Header.Del("Cookie")
			if request.In.Method != http.MethodPost || request.In.URL.Path != "/ingest" {
				request.Out.Header.Del("Authorization")
				request.Out.Header.Del("X-App-ID")
				query := request.Out.URL.Query()
				query.Del("token")
				request.Out.URL.RawQuery = query.Encode()
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			supportError(w, http.StatusServiceUnavailable, "HH Support is unavailable")
		},
	}
	service.handler = http.StripPrefix("/support", proxy)
	return service
}

func (service *supportProxy) issueTicket(w http.ResponseWriter, r *http.Request) {
	service.mu.Lock()
	defer service.mu.Unlock()
	now := time.Now()
	for ticket, expires := range service.tickets {
		if !expires.After(now) {
			delete(service.tickets, ticket)
		}
	}
	if len(service.tickets) >= 1024 {
		supportError(w, http.StatusServiceUnavailable, "Too many pending live connections")
		return
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		supportError(w, http.StatusServiceUnavailable, "Unable to create a live connection ticket")
		return
	}
	ticket := base64.RawURLEncoding.EncodeToString(token[:])
	service.tickets[ticket] = now.Add(30 * time.Second)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"ticket": ticket})
}

func (service *supportProxy) websocket(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (!strings.EqualFold(parsed.Host, r.Host) && !service.origins[origin]) {
			supportError(w, http.StatusForbidden, "Live connection origin is not allowed")
			return
		}
	}
	if !isWebSocketRequest(r) {
		supportError(w, http.StatusBadRequest, "A WebSocket upgrade is required")
		return
	}
	ticket := r.URL.Query().Get("ticket")
	service.mu.Lock()
	expires, found := service.tickets[ticket]
	delete(service.tickets, ticket)
	service.mu.Unlock()
	if !found || !expires.After(time.Now()) {
		supportError(w, http.StatusUnauthorized, "Live connection ticket is invalid or expired")
		return
	}
	request := r.Clone(r.Context())
	query := request.URL.Query()
	query.Del("ticket")
	query.Del("token")
	request.URL.RawQuery = query.Encode()
	service.handler.ServeHTTP(w, request)
}

func setupSupportAPI(router chi.Router) {
	service := newSupportProxy(os.Getenv("SUPPORT_URL"), os.Getenv("SUPPORT_PROXY_SECRET"), os.Getenv("SUPPORT_ALLOWED_ORIGINS"))
	router.Post("/support/ingest", service.handler.ServeHTTP)
	router.Get("/support/ws", service.websocket)
	router.With(authMiddleware).Post("/support/ws-ticket", service.issueTicket)
	router.With(authMiddleware).Handle("/support", service.handler)
	router.With(authMiddleware).Handle("/support/*", service.handler)
}

func stripAPIPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			request := r.Clone(r.Context())
			request.URL.Path = strings.TrimPrefix(request.URL.Path, "/api")
			if request.URL.Path == "" {
				request.URL.Path = "/"
			}
			if request.URL.RawPath != "" {
				request.URL.RawPath = strings.TrimPrefix(request.URL.RawPath, "/api")
			}
			r = request
		}
		next.ServeHTTP(w, r)
	})
}

func supportSafeRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if path == "/support" || strings.HasPrefix(path, "/support/") {
			request := r.Clone(r.Context())
			request.RequestURI = request.URL.EscapedPath()
			r = request
		}
		next.ServeHTTP(w, r)
	})
}
