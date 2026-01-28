package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func listAccessLists(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	lists, err := npm.ListAccessLists(baseURL, loginToken)
	if err != nil {
		http.Error(w, "failed to get access lists: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lists); err != nil {
		http.Error(w, "failed to marshal access lists", http.StatusInternalServerError)
		return
	}
}

func createAccessList(w http.ResponseWriter, r *http.Request) {
	var list npm.AccessList
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if _, err := npm.CreateAccessList(baseURL, loginToken, list); err != nil {
		http.Error(w, "failed to create access list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func editAccessList(w http.ResponseWriter, r *http.Request) {
	var list npm.AccessList
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.EditAccessList(baseURL, loginToken, list); err != nil {
		http.Error(w, "failed to edit access list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteAccessList(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	if err := npm.DeleteAccessList(baseURL, loginToken, payload.ID); err != nil {
		http.Error(w, "failed to delete access list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func SetupAccessListAPI(r chi.Router) chi.Router {
	return r.Route("/access-lists", func(r chi.Router) {
		r.Get("/list", listAccessLists)
		r.Post("/create", createAccessList)
		r.Put("/edit", editAccessList)
		r.Delete("/delete", deleteAccessList)
	})
}
