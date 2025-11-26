package api

import (
	"512SvMan/api/npmapi"
	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/nots"
	"512SvMan/npm"
	"512SvMan/services"
	ws "512SvMan/websocket"
	"encoding/json"
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

func SetCookieInBrowser(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:   "Authorization",
		Value:  token,
		MaxAge: maxAge,
		Path:   "/",
	})
}

func GetCookieFromRequest(r *http.Request) (string, error) {
	cookie, err := r.Cookie("Authorization")
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func register_nots(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != "" {
		SetCookieInBrowser(w, token, 3600)
	}
	loginService := services.LoginService{}
	if !loginService.IsLoginValid(baseURL, token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.ServeFile(w, r, "static/nots.html")
}

// GET /notification/public-key → devolve a VAPID public key em JSON
func get_public_key(w http.ResponseWriter, r *http.Request) {
	pub := env512.VapidPublicKey
	if pub == "" {
		http.Error(w, "VAPID public key não configurada", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"publicKey": pub,
	}); err != nil {
		http.Error(w, "failed to encode public key: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// POST /notification/subscribe → recebe subscription e guarda globalmente
func subscribe_nots(w http.ResponseWriter, r *http.Request) {
	var sub db.PushSubscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := db.DbSaveSubscription(sub); err != nil {
		http.Error(w, "failed to save subscription: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// DELETE /notification/subscriptions -> remove all stored push subscriptions
func delete_all_subscriptions(w http.ResponseWriter, r *http.Request) {
	if err := db.DbDeleteAllSubscriptions(); err != nil {
		http.Error(w, "failed to delete subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// POST /notification/test → envia notificação para TODOS
func test_nots(w http.ResponseWriter, r *http.Request) {

	if err := nots.SendGlobalNotification("HyperHive", "Isto é uma notificação de teste.", "/", true); err != nil {
		http.Error(w, "failed to send notifications: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"sent"}`))
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

	//tem de estar fora a autenticacao esta dentro da rota
	//é das notifications hehe :D:D:D:D:D::D:D:D:D q funfaoummm
	r.Get("/nots/register", register_nots)
	r.Get("/static/notification-icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, "static/notification-icon.png")
	})

	//create a group protected by auth middleware
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		setupNoVNCAPI(r)

		//NOTIFICATION heere HEHEHE
		r.Route("/notification", func(r chi.Router) {
			r.Get("/public-key", get_public_key)
			r.Post("/subscribe", subscribe_nots)
			r.Get("/test", test_nots) // opcional, para testes
			r.Delete("/subscriptions", delete_all_subscriptions)
		})
		r.Get("/sw.js", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "static/sw.js")
		})

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
		setupSmartDiskAPI(r)
		setupGoAccessAPI(r)
		setupStreamInfo(r)
		setupWireguardAPI(r)
		setupBTRFS(r)
		setupNotsAPI(r)
	})

	http.ListenAndServe(":9595", r)
}
