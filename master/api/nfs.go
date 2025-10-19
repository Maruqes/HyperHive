package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"encoding/json"
	"net/http"

	proto "github.com/Maruqes/512SvMan/api/proto/nfs"
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

	type resStruct struct {
		NfsShare db.NFSShare
		Status   *proto.SharedFolderStatusResponse
	}

	res := make([]resStruct, 0, len(shares))
	//for each share get GetSharedFolderStatus and add to share
	for i := range shares {
		status, err := nfsService.GetSharedFolderStatus(&proto.FolderMount{
			MachineName: shares[i].MachineName,
			FolderPath:  shares[i].FolderPath,
			Source:      shares[i].Source,
			Target:      shares[i].Target,
		})
		if err != nil {
			logger.Error("GetSharedFolderStatus failed: %v", err)
			res = append(res, resStruct{
				NfsShare: shares[i],
				Status: &proto.SharedFolderStatusResponse{
					Working:         false,
					SpaceOccupiedGB: -1,
					SpaceFreeGB:     -1,
					SpaceTotalGB:    -1,
				},
			})
		} else {
			res = append(res, resStruct{
				NfsShare: shares[i],
				Status:   status,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(res)
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

	force := false
	if r.URL.Query().Get("force") == "true" {
		force = true
	}

	err := nfsService.DeleteSharePoint(force)
	if err != nil {
		logger.Error("DeleteSharePoint failed: %v", err)
		http.Error(w, "failed to delete share point: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func listPathContents(w http.ResponseWriter, r *http.Request) {
	machine := chi.URLParam(r, "machine")

	type listPathContentsRequest struct {
		Path string `json:"path"`
	}
	var req listPathContentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	nfsService := services.NFSService{}
	contents, err := nfsService.ListFolderContents(machine, req.Path)
	if err != nil {
		logger.Error("ListFolderContents failed: %v", err)
		http.Error(w, "failed to list folder contents: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(contents)
	if err != nil {
		http.Error(w, "failed to marshal folder contents", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(data)
}

func setupNFSAPI(r chi.Router) chi.Router {
	return r.Route("/nfs", func(r chi.Router) {
		r.Get("/list", listShares)
		r.Post("/create", createShare)
		r.Delete("/delete", deleteShare)
		r.Get("/contents/{machine}", listPathContents)
	})
}
