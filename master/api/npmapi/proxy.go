package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listProxies(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	proxies, err := npm.GetAllProxys(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to get proxies: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proxies); err != nil {
		http.Error(w, "failed to marshal proxies", http.StatusInternalServerError)
		return
	}
}

func createProxy(w http.ResponseWriter, r *http.Request) {
	var p npm.Proxy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if _, err := npm.CreateProxy(baseURL, loginToken, p); err != nil {
		http.Error(w, "failed to create proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func editProxy(w http.ResponseWriter, r *http.Request) {
	var p npm.Proxy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.EditProxy(baseURL, loginToken, p); err != nil {
		http.Error(w, "failed to edit proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func deleteProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.DeleteProxy(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to delete proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func disableProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.DisableProxy(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to disable proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func enableProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.EnableProxy(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to enable proxy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func SetupProxyAPI(r chi.Router) chi.Router {
	return r.Route("/proxy", func(r chi.Router) {
		r.Get("/list", listProxies)
		r.Post("/create", createProxy)
		r.Put("/edit", editProxy)
		r.Delete("/delete", deleteProxy)
		r.Post("/disable", disableProxy)
		r.Post("/enable", enableProxy)
	})
}
