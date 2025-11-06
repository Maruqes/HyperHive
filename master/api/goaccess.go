package api

import (
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

	"512SvMan/env512"

	"github.com/go-chi/chi/v5"
)

const (
	goAccessTimeout       = 2 * time.Minute
	goAccessRefreshSecond = 5
	goAccessGeoIPDir      = "geoipdb"
)

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

	addPanelArgs := func(flag string, values []string) {
		for _, panel := range values {
			panel = strings.TrimSpace(panel)
			if panel == "" {
				continue
			}
			args = append(args, fmt.Sprintf("--%s=%s", flag, panel))
		}
	}
	addPanelArgs("enable-panel", env512.GoAccessEnablePanels)
	addPanelArgs("disable-panel", env512.GoAccessDisablePanels)

	geoIPDBPath, err := resolveGeoIPDatabase(workDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	args = append(args, "--geoip-database="+geoIPDBPath)

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

func resolveGeoIPDatabase(workDir string) (string, error) {
	configured := strings.TrimSpace(env512.GoAccessGeoIPDB)
	if configured != "" {
		path := configured
		if !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		stat, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("configured GeoIP database %q is not accessible: %w", path, err)
		}
		if stat.IsDir() {
			return "", fmt.Errorf("configured GeoIP database path %q is a directory", path)
		}
		return path, nil
	}

	dir := filepath.Join(workDir, goAccessGeoIPDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("unable to prepare GeoIP directory %q: %w", dir, err)
	}

	preferred := []string{
		"GeoLite2-City.mmdb",
		"GeoLite2-Country.mmdb",
		"GeoIP2-City.mmdb",
		"GeoIP2-Country.mmdb",
	}

	for _, name := range preferred {
		candidate := filepath.Join(dir, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.mmdb"))
	if err != nil {
		return "", fmt.Errorf("scan GeoIP directory %q: %w", dir, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("GeoIP database not found in %s. Download GeoLite2 (or equivalent) and place the .mmdb file there or set GOACCESS_GEOIP_DB", dir)
	}
	return files[0], nil
}
