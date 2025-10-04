package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listStream(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	streams, err := npm.ListStreams(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to get Stream hosts: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(streams); err != nil {
		http.Error(w, "failed to marshal streams", http.StatusInternalServerError)
		return
	}
}

func createStream(w http.ResponseWriter, r *http.Request) {
	var p npm.Stream
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	_, err := npm.CreateStream(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to create Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func editStream(w http.ResponseWriter, r *http.Request) {
	var p npm.Stream
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.EditStream(baseURL, loginToken, p)
	if err != nil {
		http.Error(w, "failed to edit Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DeleteStream(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to delete Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func disableStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DisableStream(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to disable Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func enableStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.EnableStream(baseURL, loginToken, payload.ID)
	if err != nil {
		http.Error(w, "failed to enable Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func SetupStreamAPI(r chi.Router) chi.Router {
	return r.Route("/stream", func(r chi.Router) {
		r.Get("/list", listStream)
		r.Post("/create", createStream)
		r.Put("/edit", editStream)
		r.Delete("/delete", deleteStream)
		r.Post("/disable", disableStream)
		r.Post("/enable", enableStream)
	})
}
