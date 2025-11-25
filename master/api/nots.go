package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"512SvMan/services"
)

// listNots handles GET /nots?since=<RFC3339|YYYY-MM-DD> and returns nots from that date until now.
func listNots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("since")
	if q == "" {
		http.Error(w, "missing 'since' query parameter (RFC3339 or YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	var since time.Time
	var err error
	since, err = time.Parse(time.RFC3339, q)
	if err != nil {
		// try short date
		since, err = time.Parse("2006-01-02", q)
		if err != nil {
			http.Error(w, "invalid 'since' parameter, use RFC3339 or YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	svc := services.NotsService{}
	nots, err := svc.GetNotsSince(since)
	if err != nil {
		http.Error(w, "failed to load nots", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nots)
}

func setupNotsAPI(r chi.Router) chi.Router {
	return r.Route("/nots", func(r chi.Router) {
		r.Get("/", listNots)
	})
}
