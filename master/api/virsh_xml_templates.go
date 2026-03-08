package api

import (
	"512SvMan/db"
	"512SvMan/services"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type vmXMLTemplateUpsertRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	XML         string `json:"xml"`
}

func setupVirshXMLTemplatesAPI(r chi.Router) {
	r.Route("/xmltemplates", func(r chi.Router) {
		r.Get("/", listVMXMLTemplates)
		r.Get("/{id}", getVMXMLTemplateByID)
		r.Post("/", createVMXMLTemplate)
		r.Put("/{id}", updateVMXMLTemplate)
		r.Delete("/{id}", deleteVMXMLTemplate)
	})
}

func listVMXMLTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := db.GetAllVMXMLTemplates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(templates)
}

func getVMXMLTemplateByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseVMXMLTemplateID(w, r)
	if !ok {
		return
	}

	tmpl, err := db.GetVMXMLTemplateByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tmpl == nil {
		http.Error(w, "vm xml template not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tmpl)
}

func createVMXMLTemplate(w http.ResponseWriter, r *http.Request) {
	var req vmXMLTemplateUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.XML = strings.TrimSpace(req.XML)

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := services.ValidateVMXMLTemplate(req.XML); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := db.AddVMXMLTemplate(r.Context(), req.Name, req.Description, req.XML)
	if err != nil {
		if isUniqueConstraintErr(err) {
			http.Error(w, "a vm xml template with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	created, err := db.GetVMXMLTemplateByID(r.Context(), int(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

func updateVMXMLTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseVMXMLTemplateID(w, r)
	if !ok {
		return
	}

	existing, err := db.GetVMXMLTemplateByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "vm xml template not found", http.StatusNotFound)
		return
	}

	var req vmXMLTemplateUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.XML = strings.TrimSpace(req.XML)

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := services.ValidateVMXMLTemplate(req.XML); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := db.UpdateVMXMLTemplate(r.Context(), id, req.Name, req.Description, req.XML); err != nil {
		if isUniqueConstraintErr(err) {
			http.Error(w, "a vm xml template with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated, err := db.GetVMXMLTemplateByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func deleteVMXMLTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseVMXMLTemplateID(w, r)
	if !ok {
		return
	}

	existing, err := db.GetVMXMLTemplateByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "vm xml template not found", http.StatusNotFound)
		return
	}

	if err := db.DeleteVMXMLTemplate(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func parseVMXMLTemplateID(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := chi.URLParam(r, "id")
	if strings.TrimSpace(idStr) == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return 0, false
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed")
}
