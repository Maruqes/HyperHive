package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/Maruqes/512SvMan/api/proto/vm_disk"
	"google.golang.org/grpc"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func listVMDisk(w http.ResponseWriter, r *http.Request) {
	service := services.VMDiskService{}
	res, err := service.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func createVMDisk(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Name   string `json:"name"`
		NFSID  int    `json:"nfs_id"`
		SizeGB int64  `json:"size_gb"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	service := services.VMDiskService{}
	res, err := service.Create(r.Context(), reqBody.Name, reqBody.NFSID, reqBody.SizeGB, reqBody.Format)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func deleteVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid vm disk id", http.StatusBadRequest)
		return
	}

	service := services.VMDiskService{}
	res, err := service.Delete(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func growVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid vm disk id", http.StatusBadRequest)
		return
	}

	var reqBody struct {
		SizeGB int64 `json:"size_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	service := services.VMDiskService{}
	res, err := service.Grow(r.Context(), id, reqBody.SizeGB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func downloadVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid vm disk id", 400)
		return
	}
	operation, err := db.BeginDiskMutation(r.Context(), id, "download")
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	succeeded := false
	defer func() {
		if succeeded {
			operation.Phase = "complete"
		} else {
			operation.Error = "download failed or client disconnected"
			operation.Phase = "uncertain"
		}
		if err := db.SaveDiskOperation(context.Background(), operation, "reserved"); err != nil {
			fmt.Printf("failed to persist disk download operation %s: %v\n", operation.ID, err)
		}
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	disk, stream, err := (&services.VMDiskService{}).Download(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	succeeded = writeDiskDownload(w, disk, stream)
}

func writeDiskDownload(w http.ResponseWriter, disk *db.VMDisk, stream grpc.ServerStreamingClient[pb.VMDiskChunk]) bool {
	header, err := stream.Header()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return false
	}
	values := header.Get("disk-length")
	if len(values) != 1 {
		http.Error(w, "agent download framing unavailable; upgrade agent", http.StatusBadGateway)
		return false
	}
	length, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || length <= 0 {
		http.Error(w, "invalid agent file length", http.StatusBadGateway)
		return false
	}
	chunk, err := stream.Recv()
	if err != nil {
		http.Error(w, fmt.Sprintf("disk stream failed before download: %v", err), http.StatusBadGateway)
		return false
	}
	ext := ".raw"
	if disk.Format == "qcow2" || disk.Format == "qcow" {
		ext = ".qcow2"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": disk.Name + ext}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	var sent int64
	for err == nil {
		if len(chunk.Data) == 0 || int64(len(chunk.Data)) > length-sent {
			panic(http.ErrAbortHandler)
		}
		// Retain one chunk until the next receive confirms success. In
		// particular, never deliver the final bytes before a successful EOF.
		next, nextErr := stream.Recv()
		if nextErr != nil && nextErr != io.EOF {
			panic(http.ErrAbortHandler)
		}
		if nextErr == io.EOF && sent+int64(len(chunk.Data)) != length {
			panic(http.ErrAbortHandler)
		}
		n, writeErr := w.Write(chunk.Data)
		sent += int64(n)
		if writeErr != nil {
			return false
		}
		chunk, err = next, nextErr
	}
	if err != io.EOF || sent != length {
		panic(http.ErrAbortHandler)
	}
	return true
}

func importVMDisk(w http.ResponseWriter, r *http.Request) {
	nfsID, err := strconv.Atoi(r.URL.Query().Get("nfs_id"))
	if err != nil || nfsID <= 0 {
		http.Error(w, "invalid nfs_id", 400)
		return
	}
	length, err := strconv.ParseInt(r.URL.Query().Get("length"), 10, 64)
	if err != nil || length <= 0 || length > services.MaxDiskImportBytes || (r.ContentLength >= 0 && r.ContentLength != length) {
		http.Error(w, "positive declared length must match request body", 400)
		return
	}
	defer r.Body.Close()
	result, err := (&services.VMDiskService{}).Import(r.Context(), r.URL.Query().Get("name"), nfsID, length, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
func moveVMDisk(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid disk ID", 400)
		return
	}
	var body struct {
		NFSID int `json:"nfs_id"`
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.NFSID <= 0 {
		http.Error(w, "valid destination nfs_id required", 400)
		return
	}
	result, err := (&services.VMDiskService{}).Move(r.Context(), id, body.NFSID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
func listDiskOperations(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := db.ListDiskOperations(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
func reconcileDiskOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "action required", 400)
		return
	}
	result, err := (&services.VMDiskService{}).Reconcile(r.Context(), chi.URLParam(r, "operation"), body.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func setupVMDiskAPI(r chi.Router) chi.Router {
	return r.Route("/vm-disk", func(r chi.Router) {
		r.Get("/list", listVMDisk)
		r.Put("/import", importVMDisk)
		r.Post("/{id}/move", moveVMDisk)
		r.Get("/operations", listDiskOperations)
		r.Post("/operations/{operation}/reconcile", reconcileDiskOperation)
		r.Get("/{id}/download", downloadVMDisk)
		r.Post("/create", createVMDisk)
		r.Delete("/{id}", deleteVMDisk)
		r.Post("/{id}/grow", growVMDisk)
	})
}
