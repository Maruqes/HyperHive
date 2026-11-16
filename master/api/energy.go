package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"512SvMan/db"
	"512SvMan/services"

	"github.com/go-chi/chi/v5"
)

const energyPriceBodyLimit = 4096

type energyPriceRequest struct {
	PricePerKWh *float64 `json:"pricePerKWh"`
}

func getEnergySummary(w http.ResponseWriter, r *http.Request) {
	summary, err := services.Energy.Summary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, summary)
}

func getEnergyTimeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	timeline, err := services.Energy.Timeline(r.Context(), q.Get("range"), q.Get("kind"), q.Get("machine"), q.Get("name"))
	if errors.Is(err, services.ErrEnergyTimelineInput) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, timeline)
}

func putEnergyPrice(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req energyPriceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, energyPriceBodyLimit)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := validateEnergyPrice(req.PricePerKWh); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := db.SetEnergyPrice(r.Context(), *req.PricePerKWh); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]float64{"pricePerKWh": *req.PricePerKWh})
}

func validateEnergyPrice(price *float64) error {
	if price == nil {
		return fmt.Errorf("pricePerKWh is required")
	}
	if math.IsNaN(*price) || math.IsInf(*price, 0) {
		return fmt.Errorf("pricePerKWh must be a finite number")
	}
	if *price < 0 {
		return fmt.Errorf("pricePerKWh must not be negative")
	}
	if *price > 10 {
		return fmt.Errorf("pricePerKWh must be at most 10")
	}
	return nil
}

func setupEnergyAPI(r chi.Router) chi.Router {
	return r.Route("/energy", func(r chi.Router) {
		r.Get("/summary", getEnergySummary)
		r.Get("/timeline", getEnergyTimeline)
		r.Put("/price", putEnergyPrice)
	})
}
