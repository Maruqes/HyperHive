package api

import (
	"512SvMan/services"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func setupCelldAPI(r chi.Router) {
	r.Get("/celld/health", getCelldHealth)
	r.Get("/celld/fleets", listCelldFleets)
	r.Post("/celld/fleets", createCelldFleet)
	r.Get("/celld/fleets/{id}", getCelldFleet)
	r.Patch("/celld/fleets/{id}", editCelldFleet)
	r.Delete("/celld/fleets/{id}", celldFleetAction("delete"))
	for _, action := range []string{"start", "stop", "restart"} {
		r.Post("/celld/fleets/{id}/"+action, celldFleetAction(action))
	}
	r.Get("/celld/fleets/{id}/credentials", getCelldCredentials)
}

func getCelldHealth(w http.ResponseWriter, r *http.Request) {
	health := services.CelldFleet.Health(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !health.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(health)
}

func celldResponse(w http.ResponseWriter, code int, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		code = http.StatusServiceUnavailable
		switch {
		case errors.Is(err, services.ErrCelldNotFound):
			code = http.StatusNotFound
		case errors.Is(err, services.ErrCelldConflict):
			code = http.StatusConflict
		case errors.Is(err, services.ErrCelldInvalid):
			code = http.StatusBadRequest
		}
		value = map[string]string{"error": err.Error()}
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func celldInput(w http.ResponseWriter, r *http.Request) (services.CelldFleetInput, bool) {
	var in services.CelldFleetInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	err := d.Decode(&in)
	if err == nil {
		var extra any
		if d.Decode(&extra) != io.EOF {
			err = services.ErrCelldInvalid
		}
	}
	if err != nil {
		celldResponse(w, 400, nil, services.ErrCelldInvalid)
		return in, false
	}
	return in, true
}

func listCelldFleets(w http.ResponseWriter, r *http.Request) {
	value, err := services.CelldFleet.List()
	celldResponse(w, 200, value, err)
}

func createCelldFleet(w http.ResponseWriter, r *http.Request) {
	in, ok := celldInput(w, r)
	if !ok {
		return
	}
	value, err := services.CelldFleet.Create(in)
	if err == nil {
		w.Header().Set("Location", "/celld/fleets/"+value.ID)
	}
	celldResponse(w, 202, value, err)
}

func getCelldFleet(w http.ResponseWriter, r *http.Request) {
	value, err := services.CelldFleet.Get(chi.URLParam(r, "id"))
	celldResponse(w, 200, value, err)
}

func editCelldFleet(w http.ResponseWriter, r *http.Request) {
	in, ok := celldInput(w, r)
	if !ok {
		return
	}
	value, err := services.CelldFleet.Update(chi.URLParam(r, "id"), in)
	celldResponse(w, 202, value, err)
}

func celldFleetAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := services.CelldFleet.Action(chi.URLParam(r, "id"), action)
		celldResponse(w, 202, value, err)
	}
}

func getCelldCredentials(w http.ResponseWriter, r *http.Request) {
	value, err := services.CelldFleet.Credentials(chi.URLParam(r, "id"))
	celldResponse(w, 200, value, err)
}
