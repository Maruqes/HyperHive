package api

import (
	"512SvMan/db"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type noteRequest struct {
	Titulo string `json:"titulo"`
	Nota   string `json:"nota"`
}

func listNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := db.GetAllNotes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, notes)
}

func getNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}

	note, err := db.GetNoteByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, note)
}

func createNote(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteRequest(w, r)
	if !ok {
		return
	}

	note, err := db.AddNote(r.Context(), req.Titulo, req.Nota)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONWithStatus(w, http.StatusCreated, note)
}

func editNote(w http.ResponseWriter, r *http.Request) {
	id, ok := noteIDFromRequest(w, r)
	if !ok {
		return
	}

	req, ok := decodeNoteRequest(w, r)
	if !ok {
		return
	}

	note, err := db.UpdateNoteByID(r.Context(), id, req.Titulo, req.Nota)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONWithStatus(w, http.StatusOK, note)
}

func decodeNoteRequest(w http.ResponseWriter, r *http.Request) (noteRequest, bool) {
	defer r.Body.Close()

	var req noteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return req, false
	}
	if strings.TrimSpace(req.Titulo) == "" {
		http.Error(w, "titulo is required", http.StatusBadRequest)
		return req, false
	}
	if strings.TrimSpace(req.Nota) == "" {
		http.Error(w, "nota is required", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

func noteIDFromRequest(w http.ResponseWriter, r *http.Request) (int, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func setupNotesAPI(r chi.Router) chi.Router {
	return r.Route("/notes", func(r chi.Router) {
		r.Get("/", listNotes)
		r.Get("/{id}", getNote)
		r.Post("/", createNote)
		r.Put("/{id}", editNote)
		r.Patch("/{id}", editNote)
	})
}
