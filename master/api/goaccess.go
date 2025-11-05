package api

import (
	"512SvMan/services"
	"net/http"
)

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	report, err := services.GenerateGoAccessReport()
	if err != nil {
		http.Error(w, "failed to render GoAccess report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(report)
}
