package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"512SvMan/automations"
	"512SvMan/services"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/go-chi/chi/v5"
)

const automationBodyLimit = 2 << 20

// automationError maps service errors to HTTP status codes.
func automationError(w http.ResponseWriter, err error) {
	var support *automations.SupportError
	switch {
	case errors.As(err, &support):
		status := support.Status
		if status >= 500 {
			status = http.StatusBadGateway
			if support.Status == http.StatusServiceUnavailable {
				status = http.StatusServiceUnavailable
			}
		}
		http.Error(w, support.Message, status)
	case errors.Is(err, automations.ErrAlreadyRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, services.ErrAutomationNotRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, automations.ErrFileExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, automations.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, automations.ErrInvalidPath), errors.Is(err, services.ErrAutomationInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		logger.Errorf("automation request failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// readAutomationJSON reads a bounded JSON body; raw bodies are validated by Support.
func readAutomationJSON(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	defer r.Body.Close()
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, automationBodyLimit))
	if err != nil || !json.Valid(data) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return nil, false
	}
	return data, true
}

func decodeAutomationJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	data, ok := readAutomationJSON(w, r)
	if !ok {
		return false
	}
	if err := json.Unmarshal(data, target); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func listAutomations(w http.ResponseWriter, r *http.Request) {
	result, err := services.AutomationService{}.List(r.Context())
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func createAutomation(w http.ResponseWriter, r *http.Request) {
	body, ok := readAutomationJSON(w, r)
	if !ok {
		return
	}
	result, err := services.AutomationService{}.Create(r.Context(), body)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusCreated, result)
}

func getAutomation(w http.ResponseWriter, r *http.Request) {
	result, err := services.AutomationService{}.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func updateAutomation(w http.ResponseWriter, r *http.Request) {
	body, ok := readAutomationJSON(w, r)
	if !ok {
		return
	}
	result, err := services.AutomationService{}.Update(r.Context(), chi.URLParam(r, "id"), body)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func deleteAutomation(w http.ResponseWriter, r *http.Request) {
	if err := (services.AutomationService{}).Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		automationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func runAutomation(w http.ResponseWriter, r *http.Request) {
	runID, err := services.AutomationService{}.Run(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

func evaluateAutomationCondition(w http.ResponseWriter, r *http.Request) {
	body, ok := readAutomationJSON(w, r)
	if !ok {
		return
	}
	result, err := services.AutomationService{}.Evaluate(r.Context(), body)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func getAutomationConditionCatalog(w http.ResponseWriter, r *http.Request) {
	result, err := services.AutomationService{}.ConditionCatalog(r.Context())
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func getAutomationSettings(w http.ResponseWriter, r *http.Request) {
	result, err := services.AutomationService{}.Settings(r.Context())
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func putAutomationSettings(w http.ResponseWriter, r *http.Request) {
	var body services.AutomationSettingsView
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	result, err := services.AutomationService{}.SaveSettings(r.Context(), body)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, result)
}

func listAutomationFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := automations.GetConfig(r.Context(), id); err != nil {
		automationError(w, err)
		return
	}
	files, err := automations.ListFiles(id)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]any{"files": files})
}

func readAutomationFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	content, err := automations.ReadFile(chi.URLParam(r, "id"), path)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"path": path, "content": content})
}

func writeAutomationFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Content *string `json:"content"`
	}
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	if body.Content == nil {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if _, err := automations.GetConfig(r.Context(), id); err != nil {
		automationError(w, err)
		return
	}
	info, err := automations.WriteFile(id, r.URL.Query().Get("path"), *body.Content)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, info)
}

func renameAutomationFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	if _, err := automations.GetConfig(r.Context(), id); err != nil {
		automationError(w, err)
		return
	}
	info, err := automations.RenameFile(id, body.From, body.To)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, info)
}

func deleteAutomationFile(w http.ResponseWriter, r *http.Request) {
	if err := automations.DeleteFile(chi.URLParam(r, "id"), r.URL.Query().Get("path")); err != nil {
		automationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getAutomationPackage(w http.ResponseWriter, r *http.Request) {
	content, err := automations.ReadPackage()
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"content": content})
}

func putAutomationPackage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	if err := automations.WritePackage(body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"content": body.Content})
}

func installAutomationPackages(w http.ResponseWriter, r *http.Request) {
	runID, err := services.AutomationService{}.Install(r.Context())
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

func getAutomationSDK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, automations.Declaration())
}

func listAutomationRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := services.AutomationService{}.Runs(r.Context(), r.URL.Query().Get("automation_id"), limit)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]any{"runs": runs})
}

func getAutomationRun(w http.ResponseWriter, r *http.Request) {
	run, err := services.AutomationService{}.RunDetail(r.Context(), chi.URLParam(r, "runID"))
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, run)
}

func killAutomationRun(w http.ResponseWriter, r *http.Request) {
	if err := (services.AutomationService{}).Kill(r.Context(), chi.URLParam(r, "runID")); err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]string{"status": "killing"})
}

func setupAutomationsAPI(r chi.Router) chi.Router {
	return r.Route("/automations", func(r chi.Router) {
		r.Get("/", listAutomations)
		r.Post("/", createAutomation)
		r.Post("/evaluate", evaluateAutomationCondition)
		r.Get("/catalog", getAutomationConditionCatalog)
		r.Get("/settings", getAutomationSettings)
		r.Put("/settings", putAutomationSettings)
		r.Get("/package", getAutomationPackage)
		r.Put("/package", putAutomationPackage)
		r.Post("/package/install", installAutomationPackages)
		r.Get("/sdk/hh.d.ts", getAutomationSDK)
		r.Get("/runs", listAutomationRuns)
		r.Get("/runs/{runID}", getAutomationRun)
		r.Post("/runs/{runID}/kill", killAutomationRun)
		r.Get("/{id}", getAutomation)
		r.Put("/{id}", updateAutomation)
		r.Delete("/{id}", deleteAutomation)
		r.Post("/{id}/run", runAutomation)
		r.Get("/{id}/files", listAutomationFiles)
		r.Get("/{id}/files/content", readAutomationFile)
		r.Put("/{id}/files/content", writeAutomationFile)
		r.Delete("/{id}/files/content", deleteAutomationFile)
		r.Post("/{id}/files/rename", renameAutomationFile)
	})
}

// supportInternalAuth accepts only HH Support, authenticated with the shared
// proxy secret. These routes can start root processes.
func supportInternalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("SUPPORT_PROXY_SECRET")
		if len(secret) < 32 || !constantTimeEqual(r.Header.Get("X-HH-Support-Key"), secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(left, right string) bool {
	leftHash, rightHash := sha256.Sum256([]byte(left)), sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func fireAutomation(w http.ResponseWriter, r *http.Request) {
	var body services.AutomationFire
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	runID, err := services.AutomationService{}.Fire(r.Context(), body)
	if err != nil {
		automationError(w, err)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

func notifyFromSupport(w http.ResponseWriter, r *http.Request) {
	var body services.AutomationNotification
	if !decodeAutomationJSON(w, r, &body) {
		return
	}
	if err := (services.AutomationService{}).Notify(r.Context(), body); err != nil {
		automationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func setupSupportInternalAPI(r chi.Router) {
	r.Route("/support-internal/automations", func(r chi.Router) {
		r.Use(supportInternalAuth)
		r.Post("/fire", fireAutomation)
		r.Post("/notify", notifyFromSupport)
	})
}

// configureAutomationSDK generates the script SDK from every registered route.
// A failure only disables script runs, so it is logged.
func configureAutomationSDK(router chi.Routes, port string) {
	routes := []automations.Route{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, automations.Route{Method: method, Pattern: route})
		return nil
	})
	if err == nil {
		err = automations.Configure(port, routes)
	}
	if err != nil {
		logger.Errorf("configure automation SDK: %v", err)
	}
}
