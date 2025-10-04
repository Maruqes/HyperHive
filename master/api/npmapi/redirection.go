package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listRedirection(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	Redirections, err := npm.ListRedirections(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to get Redirection hosts: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(Redirections); err != nil {
		http.Error(w, "failed to marshal Redirections", http.StatusInternalServerError)
		return
	}
}

func createRedirection(w http.ResponseWriter, r *http.Request) {
	var p npm.Redirection
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	_, err := npm.CreateRedirection(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to create Redirection host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func editRedirection(w http.ResponseWriter, r *http.Request) {
	var p npm.Redirection
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.EditRedirection(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to edit Redirection host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteRedirection(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DeleteRedirection(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to delete Redirection host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func disableRedirection(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DisableRedirection(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to disable Redirection host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func enableRedirection(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.EnableRedirection(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to enable Redirection host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func SetupRedirectionAPI(r chi.Router) chi.Router {
	return r.Route("/redirection", func(r chi.Router) {
		r.Get("/list", listRedirection)
		r.Post("/create", createRedirection)
		r.Put("/edit", editRedirection)
		r.Delete("/delete", deleteRedirection)
		r.Post("/disable", disableRedirection)
		r.Post("/enable", enableRedirection)
	})
}
