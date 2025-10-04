package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func list404(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	four04s, err := npm.List404(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to get 404 hosts: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(four04s); err != nil {
		http.Error(w, "failed to marshal proxies", http.StatusInternalServerError)
		return
	}
}

func create404(w http.ResponseWriter, r *http.Request) {
	var p npm.Host404
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	_, err := npm.Create404(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to create 404 host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func edit404(w http.ResponseWriter, r *http.Request) {
	var p npm.Host404
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.Edit404(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to edit 404 host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func delete404(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.Delete404(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to delete 404 host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func disable404(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.Disable404(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to disable 404 host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func enable404(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.Enable404(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to enable 404 host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func Setup404API(r chi.Router) chi.Router {
	return r.Route("/404", func(r chi.Router) {
		r.Get("/list", list404)
		r.Post("/create", create404)
		r.Put("/edit", edit404)
		r.Delete("/delete", delete404)
		r.Post("/disable", disable404)
		r.Post("/enable", enable404)
	})
}
