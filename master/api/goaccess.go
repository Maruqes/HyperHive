package api

import (
	"archive/tar"
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
	"sync"
	"time"

	"512SvMan/env512"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

const (
	goAccessTimeout        = 2 * time.Minute
	goAccessRefreshSecond  = 5
	goAccessGeoIPDir       = "geoipdb"
	goAccessGeoIPMaxAge    = 7 * 24 * time.Hour
	goAccessWSPort         = "7890" // GoAccess WebSocket port
	goAccessReconnectDelay = 5 * time.Second
)

var (
	errGeoIPNotFound = errors.New("geoip database not found")

	// WebSocket upgrader for client connections
	wsUpgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins, adjust for production
		},
	}

	// GoAccess process management
	goAccessCmd     *exec.Cmd
	goAccessMu      sync.Mutex
	goAccessRunning bool
)

// goAccessHandler serves the HTML page with WebSocket connection
func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	workDir, err := os.Getwd()
	if err != nil {
		http.Error(w, "failed to resolve working directory", http.StatusInternalServerError)
		return
	}

	// Ensure GoAccess is running
	if err := ensureGoAccessRunning(r.Context(), workDir); err != nil {
		http.Error(w, fmt.Sprintf("failed to start GoAccess: %v", err), http.StatusInternalServerError)
		return
	}

	statsDir := filepath.Join(workDir, "npm-data", "stats")
	outputPath := filepath.Join(statsDir, "goaccess.html")

	data, err := os.ReadFile(outputPath)
	if err != nil {
		http.Error(w, "failed to read generated report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// goAccessWebSocketHandler proxies WebSocket connections to GoAccess
func goAccessWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade client connection to WebSocket
	clientConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade connection", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Connect to GoAccess WebSocket server
	goAccessURL := fmt.Sprintf("ws://localhost:%s", goAccessWSPort)
	goAccessConn, _, err := websocket.DefaultDialer.Dial(goAccessURL, nil)
	if err != nil {
		clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error connecting to GoAccess: %v", err)))
		return
	}
	defer goAccessConn.Close()

	// Channel to signal completion
	done := make(chan struct{})

	// Forward messages from GoAccess to client
	go func() {
		defer close(done)
		for {
			messageType, message, err := goAccessConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(messageType, message); err != nil {
				return
			}
		}
	}()

	// Forward messages from client to GoAccess (for interactivity)
	go func() {
		for {
			messageType, message, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := goAccessConn.WriteMessage(messageType, message); err != nil {
				return
			}
		}
	}()

	<-done
}

// ensureGoAccessRunning starts GoAccess in background if not already running
func ensureGoAccessRunning(ctx context.Context, workDir string) error {
	goAccessMu.Lock()
	defer goAccessMu.Unlock()

	if goAccessRunning && goAccessCmd != nil && goAccessCmd.Process != nil {
		return nil
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	if stat, err := os.Stat(logDir); err != nil || !stat.IsDir() {
		return fmt.Errorf("logs directory not found")
	}

	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to enumerate log files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no proxy access logs found")
	}

	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("failed to prepare stats directory: %w", err)
	}

	outputPath := filepath.Join(statsDir, "goaccess.html")

	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Get GeoIP database
	geoIPDBPath, err := ensureGeoIPDatabase(ctx, workDir)
	if err != nil {
		return fmt.Errorf("failed to get GeoIP database: %w", err)
	}

	// Build GoAccess arguments for WebSocket mode
	args := append([]string{}, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--real-time-html",                        // Enable real-time WebSocket mode
		"--ws-url=ws://localhost:"+goAccessWSPort, // WebSocket URL
		"--port="+goAccessWSPort,                  // WebSocket port
		"--log-format="+logFormat,
		"--geoip-database="+geoIPDBPath,
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

	goAccessCmd = exec.Command("goaccess", args...)
	goAccessCmd.Dir = logDir

	// Start the process
	if err := goAccessCmd.Start(); err != nil {
		return fmt.Errorf("failed to start GoAccess: %w", err)
	}

	goAccessRunning = true

	// Monitor the process in background
	go func() {
		goAccessCmd.Wait()
		goAccessMu.Lock()
		goAccessRunning = false
		goAccessMu.Unlock()

		// Auto-restart after delay
		time.Sleep(goAccessReconnectDelay)
		ensureGoAccessRunning(context.Background(), workDir)
	}()

	// Wait a bit for GoAccess to initialize
	time.Sleep(1 * time.Second)

	return nil
}

// StopGoAccess gracefully stops the GoAccess process
func StopGoAccess() {
	goAccessMu.Lock()
	defer goAccessMu.Unlock()

	if goAccessCmd != nil && goAccessCmd.Process != nil {
		goAccessCmd.Process.Kill()
		goAccessCmd = nil
		goAccessRunning = false
	}
}

func ensureGeoIPDatabase(ctx context.Context, workDir string) (string, error) {
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

func setupGoAccessAPI(r chi.Router) {
	r.Get("/goaccess", goAccessHandler)
	r.Get("/goaccess/ws", goAccessWebSocketHandler) // WebSocket endpoint for real-time updates
}
