package api

import (
	"net/http"

	"github.com/evangwt/go-vncproxy"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

//uses https://github.com/evangwt/go-vncproxy

var vp *vncproxy.Proxy

// http://localhost:9595/novnc/vnc.html?path=/novnc/ws?token=vm1
func initNoVNC() {
	vp = vncproxy.New(&vncproxy.Config{
		LogLevel: vncproxy.DebugLevel,
		TokenHandler: func(r *http.Request) (string, error) {
			// map token -> VNC backend
			// e.g., token=vm1 -> localhost:5901 (adjust as needed)
			switch r.URL.Query().Get("token") {
			case "vm1":
				return "127.0.0.1:5900", nil
			default:
				return "", http.ErrNoLocation
			}
		},
	})
}

func serveNoVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(vp.ServeWS).ServeHTTP(w, r)
}

func serveNoVNC(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/novnc", http.FileServer(http.Dir("./novnc"))).ServeHTTP(w, r)
}

func setupNoVNCAPI(r chi.Router) chi.Router {
	return r.Route("/novnc", func(r chi.Router) {
		r.Get("/ws", serveNoVNCWebSocket)
		r.Get("/*", serveNoVNC)
	})
}
