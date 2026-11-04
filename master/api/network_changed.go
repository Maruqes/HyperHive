package api

import (
	"net/http"
	"sync"
	"time"

	"512SvMan/websocket"

	"github.com/go-chi/chi/v5/middleware"
)

// Process-lifetime debounce state: pending means a send is already scheduled.
var (
	networkChangedMu      sync.Mutex
	networkChangedPending bool
	networkChangedDelay   = time.Second
	networkChangedSend    = func() {
		websocket.BroadcastMessage(websocket.Message{Type: "NetworkChanged"})
	}
)

// broadcastNetworkChanged tells open network maps to reload after a successful write.
func broadcastNetworkChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reads pass through unwrapped, so websocket upgrades (always GET) keep the raw writer.
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		// Status is 0 when the handler wrote nothing, which net/http sends as 200.
		if status := ww.Status(); status == 0 || (status >= 200 && status < 300) {
			scheduleNetworkChanged()
		}
	})
}

func scheduleNetworkChanged() {
	networkChangedMu.Lock()
	defer networkChangedMu.Unlock()
	if networkChangedPending {
		return
	}
	networkChangedPending = true
	send := networkChangedSend
	time.AfterFunc(networkChangedDelay, func() {
		// Clear before sending, so a write during the send schedules another message.
		networkChangedMu.Lock()
		networkChangedPending = false
		networkChangedMu.Unlock()
		send()
	})
}
