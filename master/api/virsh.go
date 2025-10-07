package api

import "github.com/go-chi/chi/v5"

func setupVirshAPI(r chi.Router) chi.Router {
	return r.Route("/virsh", func(r chi.Router) {
		r.Get("/getcpufeatures", listShares)
	})
}
