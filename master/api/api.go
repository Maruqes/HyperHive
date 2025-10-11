package api

import (
	"512SvMan/api/npmapi"
	"512SvMan/npm"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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

	r.Post("/login", loginHandler)

	//testing
	r.Group(func(r chi.Router) {
	})

	//create a group protected by auth middleware
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		setupNoVNCAPI(r)
		r.Get("/protected", protectedRoutes)
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
	})

	//put web folder on /
	r.Handle("/*", http.FileServer(http.Dir("./web")))

	http.ListenAndServe(":9595", r)
}
