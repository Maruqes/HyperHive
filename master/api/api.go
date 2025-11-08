package api

import (
	"512SvMan/api/npmapi"
	"512SvMan/npm"
	ws "512SvMan/websocket"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all origins (customize for production)
		return true
	},
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ws.RegisterConnection(conn)

	// Keep the connection open
	for {
		if _, _, err := conn.NextReader(); err != nil {
			break
		}
	}
}

var baseURL string

func protectedRoutes(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("This is a protected route"))
}

func StartApi() {
	hostAdmin := "127.0.0.1:81"
	baseURL = "http://" + hostAdmin
	initNoVNC()
	err := npm.SetupNPM(baseURL)

	if err != nil {
		panic(err)
	}

	npmapi.SetBaseURL(baseURL)

	r := chi.NewRouter()

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Post("/login", loginHandler)

	//create a group protected by auth middleware
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		setupNoVNCAPI(r)
		r.Get("/protected", protectedRoutes)

		r.Get("/ws", wsHandler)

		npmapi.SetupProxyAPI(r)
		npmapi.Setup404API(r)
		npmapi.SetupStreamAPI(r)
		npmapi.SetupRedirectionAPI(r)
		npmapi.SetupCertAPI(r)

		setupVirshAPI(r)
		setupNFSAPI(r)
		setupProtocolAPI(r)
		setupLogsAPI(r)
		setupISOAPI(r)
		setupExtraAPI(r)
		setupInfoAPI(r)
		setupGoAccessAPI(r)
		setupStreamInfo(r)
		setupWireguardAPI(r)
	})

	http.ListenAndServe(":9595", r)
}
