package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	unauthenticatedRequestsPerSecond = 1.0
	unauthenticatedBurst             = 10
	visitorExpiry                    = 10 * time.Minute
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors      = make(map[string]*visitor)
	visitorsMu    sync.Mutex
	cleanupTicker *time.Ticker
	cleanupOnce   sync.Once
)

func startRateLimiterCleanup() {
	cleanupOnce.Do(func() {
		cleanupTicker = time.NewTicker(time.Minute)
		go func() {
			for range cleanupTicker.C {
				cleanupVisitors()
			}
		}()
	})
}

func cleanupVisitors() {
	now := time.Now()

	visitorsMu.Lock()
	defer visitorsMu.Unlock()

	for ip, v := range visitors {
		if now.Sub(v.lastSeen) > visitorExpiry {
			delete(visitors, ip)
		}
	}
}

func getLimiterForIP(ip string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()

	v, exists := visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(rate.Limit(unauthenticatedRequestsPerSecond), unauthenticatedBurst)
		visitors[ip] = &visitor{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

func clientIP(r *http.Request) string {
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}

	return r.RemoteAddr
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, authorized := ensureAuthorizedContext(r)
		if authorized {
			next.ServeHTTP(w, r)
			return
		}

		if !getLimiterForIP(clientIP(r)).Allow() {
			applyCORSHeaders(w, r)
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
