package api

import (
	"512SvMan/services"
	"encoding/json"
	"net/http"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

func listShares(w http.ResponseWriter, r *http.Request) {
	nfsService := services.NFSService{}

	shares, err := nfsService.GetAllSharedFolders()
	if err != nil {
		http.Error(w, "failed to get shares: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(shares)
	if err != nil {
		http.Error(w, "failed to marshal shares", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(data)
}

func createShare(w http.ResponseWriter, r *http.Request) {
	nfsService := services.NFSService{}
	//get from request body
	if err := json.NewDecoder(r.Body).Decode(&nfsService.SharePoint); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	err := nfsService.CreateSharePoint()
	if err != nil {
		logger.Error("CreateSharePoint failed: %v", err)
		http.Error(w, "failed to create share point: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteShare(w http.ResponseWriter, r *http.Request) {

	nfsService := services.NFSService{}
	if err := json.NewDecoder(r.Body).Decode(&nfsService.SharePoint); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	err := nfsService.DeleteSharePoint()
	if err != nil {
		logger.Error("DeleteSharePoint failed: %v", err)
		http.Error(w, "failed to delete share point: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func setupNFSAPI(r chi.Router) chi.Router {
	return r.Route("/nfs", func(r chi.Router) {
		r.Get("/list", listShares)
		r.Post("/create", createShare)
		r.Delete("/delete", deleteShare)
	})
}
