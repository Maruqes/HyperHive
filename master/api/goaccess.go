package api

import (
	"512SvMan/env512"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5
)

// parsePanels recebe uma string (ex.: "VISITORS, requests,  status_codes")
// e devolve uma lista normalizada para flags do GoAccess.
func parsePanels(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	sep := func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '|' }
	raw := strings.FieldsFunc(s, sep)

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// GoAccess espera nomes de painel em MAIÚSCULAS, p.ex. REQUESTS, STATUS_CODES, REALTIME, etc.
		key := strings.ToUpper(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		http.Error(w, "failed to resolve working directory", http.StatusInternalServerError)
		return
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	if stat, err := os.Stat(logDir); err != nil || !stat.IsDir() {
		http.Error(w, "logs directory not found", http.StatusNotFound)
		return
	}

	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		http.Error(w, "failed to enumerate log files", http.StatusInternalServerError)
		return
	}
	if len(files) == 0 {
		http.Error(w, "no proxy access logs found", http.StatusNotFound)
		return
	}

	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		http.Error(w, "failed to prepare stats directory", http.StatusInternalServerError)
		return
	}

	outputPath := filepath.Join(statsDir, "goaccess.html")

	// Formato compatível com logs do Nginx Proxy Manager
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		fmt.Sprintf("--html-refresh=%d", goAccessRefreshSecond),
		"--log-format="+logFormat,
		"-o", outputPath,
	)

	// ---- Controlo de painéis via env512 ----
	enable := env512.GoAccessDisablePanels
	disable := env512.GoAccessDisablePanels

	// Se AMBOS vazios → não adicionar flags -> GoAccess mantém todos os painéis por defeito.
	if len(enable) > 0 {
		for _, p := range enable {
			args = append(args, "--enable-panel="+p)
		}
	}
	if len(disable) > 0 {
		for _, p := range disable {
			args = append(args, "--ignore-panel="+p)
		}
	}
	// ----------------------------------------

	cmd := exec.CommandContext(ctx, "goaccess", args...)
	cmd.Dir = logDir

	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "goaccess timed out while generating report", http.StatusGatewayTimeout)
			return
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		http.Error(w, fmt.Sprintf("failed to generate goaccess report: %s", msg), http.StatusInternalServerError)
		return
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
}
