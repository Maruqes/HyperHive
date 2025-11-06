package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	goAccessGeoIPMaxAge   = 7 * 24 * time.Hour
)

var errGeoIPNotFound = errors.New("geoip database not found")

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

	geoIPDBPath, err := ensureGeoIPDatabase(ctx, workDir)
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

func ensureGeoIPDatabase(ctx context.Context, workDir string) (string, error) {
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

	cached, err := findCachedGeoIP(workDir)
	if errors.Is(err, errGeoIPNotFound) {
		return downloadGeoIPDatabase(ctx, workDir)
	}
	if err != nil {
		return "", err
	}

	info, err := os.Stat(cached)
	if err != nil {
		if os.IsNotExist(err) {
			return downloadGeoIPDatabase(ctx, workDir)
		}
		return "", fmt.Errorf("inspect cached GeoIP database: %w", err)
	}

	if time.Since(info.ModTime()) > goAccessGeoIPMaxAge {
		if strings.TrimSpace(env512.GoAccessGeoIPLicenseKey) == "" {
			return cached, nil
		}
		freshPath, err := downloadGeoIPDatabase(ctx, workDir)
		if err != nil {
			return "", fmt.Errorf("refresh GeoIP database: %w", err)
		}
		return freshPath, nil
	}

	return cached, nil
}

func findCachedGeoIP(workDir string) (string, error) {
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
		return "", fmt.Errorf("%w: %s", errGeoIPNotFound, dir)
	}
	return files[0], nil
}

func downloadGeoIPDatabase(ctx context.Context, workDir string) (string, error) {
	key := strings.TrimSpace(env512.GoAccessGeoIPLicenseKey)
	if key == "" {
		return "", fmt.Errorf("GOACCESS_GEOIP_LICENSE_KEY must be set to download GeoIP databases automatically")
	}

	edition := env512.GoAccessGeoIPEdition
	if edition == "" {
		edition = "GeoLite2-City"
	}

	dir := filepath.Join(workDir, goAccessGeoIPDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("unable to prepare GeoIP directory %q: %w", dir, err)
	}

	downloadURL := fmt.Sprintf(
		"https://download.maxmind.com/app/geoip_download?edition_id=%s&license_key=%s&suffix=tar.gz",
		url.QueryEscape(edition),
		url.QueryEscape(key),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("construct GeoIP download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download GeoIP database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("download GeoIP database: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decompress GeoIP archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var extracted string

	for {
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("read GeoIP archive: %w", err)
		}

		if header.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
			continue
		}

		base := filepath.Base(header.Name)
		tmp, err := os.CreateTemp(dir, "geoip-*.mmdb")
		if err != nil {
			return "", fmt.Errorf("create temp GeoIP file: %w", err)
		}

		if _, err := io.Copy(tmp, tr); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write GeoIP database: %w", err)
		}
		tmp.Close()

		dest := filepath.Join(dir, base)
		_ = os.Remove(dest)
		if err := os.Rename(tmp.Name(), dest); err != nil {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("place GeoIP database: %w", err)
		}

		extracted = dest
		break
	}

	if extracted == "" {
		return "", fmt.Errorf("downloaded GeoIP archive but no .mmdb file was found")
	}

	return extracted, nil
}
