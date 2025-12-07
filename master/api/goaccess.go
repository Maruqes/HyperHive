package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
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
	goAccessGeoIPDir       = "geoipdb"
	goAccessGeoIPMaxAge    = 7 * 24 * time.Hour
	goAccessWSPort         = "7890"         // GoAccess WebSocket port (internal)
	goAccessWSRoute        = "/ws_goaccess" // Public WebSocket route
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
	goAccessCmd       *exec.Cmd
	goAccessMu        sync.Mutex
	goAccessRunning   bool
	goAccessLogFiles  []string      // Track current log files
	goAccessWatchStop chan struct{} // Signal to stop file watcher
)

// goAccessHandler serves the HTML page with WebSocket connection
func goAccessHandler(w http.ResponseWriter, r *http.Request) {
	workDir, err := os.Getwd()
	if err != nil {
		http.Error(w, "failed to resolve working directory", http.StatusInternalServerError)
		return
	}

	// Ensure GoAccess is running
	ctx := keepAliveCtx(r)

	if err := ensureGoAccessRunning(ctx, workDir); err != nil {
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

// buildWebSocketURL constructs the WebSocket URL using MAIN_LINK
func buildWebSocketURL() string {
	mainLink := strings.TrimSpace(env512.MAIN_LINK)
	if mainLink == "" {
		// Fallback to relative URL if MAIN_LINK is not set
		return goAccessWSRoute
	}

	// Remove trailing slash from MAIN_LINK
	mainLink = strings.TrimRight(mainLink, "/")

	// Determine protocol (ws:// or wss://) and default port
	var wsProtocol string
	var defaultPort string

	if strings.HasPrefix(mainLink, "https://") {
		wsProtocol = "wss://"
		defaultPort = "443"
		mainLink = strings.TrimPrefix(mainLink, "https://")
	} else if strings.HasPrefix(mainLink, "http://") {
		wsProtocol = "ws://"
		defaultPort = "80"
		mainLink = strings.TrimPrefix(mainLink, "http://")
	} else {
		// No protocol specified, assume HTTPS
		wsProtocol = "wss://"
		defaultPort = "443"
	}

	// Check if port is already specified
	hasPort := strings.Contains(mainLink, ":")

	// Add default port if none specified
	if !hasPort {
		// Check if there's a path in the URL
		if idx := strings.Index(mainLink, "/"); idx != -1 {
			// Insert port before the path
			mainLink = mainLink[:idx] + ":" + defaultPort + mainLink[idx:]
		} else {
			// No path, just append port
			mainLink = mainLink + ":" + defaultPort
		}
	}

	// Build full WebSocket URL
	return wsProtocol + mainLink + goAccessWSRoute
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

	logFormat := `[%d:%t %^] %^ %^ %s %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Get GeoIP database
	geoIPDBPath, err := ensureGeoIPDatabase(ctx, workDir)
	if err != nil {
		return fmt.Errorf("failed to get GeoIP database: %w", err)
	}

	// Build WebSocket URL for clients using MAIN_LINK
	wsURL := buildWebSocketURL()

	// Build GoAccess arguments for WebSocket mode
	args := []string{
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--real-time-html",         // Enable real-time WebSocket mode
		"--ws-url=" + wsURL,        // WebSocket URL for clients (uses MAIN_LINK + /ws_goaccess)
		"--port=" + goAccessWSPort, // Internal WebSocket port for GoAccess
		"--log-format=" + logFormat,
		"--geoip-database=" + geoIPDBPath,
		"-o", outputPath,
	}
	args = append(args, files...)

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

	// Capture stdout and stderr for debugging
	logFile := filepath.Join(statsDir, "goaccess.log")
	logWriter, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		goAccessCmd.Stdout = logWriter
		goAccessCmd.Stderr = logWriter
	}

	// Start the process
	if err := goAccessCmd.Start(); err != nil {
		if logWriter != nil {
			logWriter.Close()
		}
		return fmt.Errorf("failed to start GoAccess: %w", err)
	}

	goAccessRunning = true
	goAccessLogFiles = files // Track current log files

	// Start file watcher if not already running
	if goAccessWatchStop == nil {
		goAccessWatchStop = make(chan struct{})
		go watchLogFiles(workDir)
	}

	// Monitor the process in background
	go func() {
		goAccessCmd.Wait()
		if logWriter != nil {
			logWriter.Close()
		}
		goAccessMu.Lock()
		goAccessRunning = false
		goAccessMu.Unlock()

		// Auto-restart after delay
		time.Sleep(goAccessReconnectDelay)
		ensureGoAccessRunning(context.Background(), workDir)
	}()

	// Wait a bit for GoAccess to initialize and start WebSocket server
	time.Sleep(2 * time.Second)

	return nil
}

// StopGoAccess gracefully stops the GoAccess process
func StopGoAccess() {
	goAccessMu.Lock()
	defer goAccessMu.Unlock()

	// Stop file watcher
	if goAccessWatchStop != nil {
		close(goAccessWatchStop)
		goAccessWatchStop = nil
	}

	if goAccessCmd != nil && goAccessCmd.Process != nil {
		goAccessCmd.Process.Kill()
		goAccessCmd = nil
		goAccessRunning = false
	}
}

// watchLogFiles monitors the log directory for new files and restarts GoAccess
func watchLogFiles(workDir string) {
	logDir := filepath.Join(workDir, "npm-data", "logs")
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-goAccessWatchStop:
			return
		case <-ticker.C:
			// Get current log files
			currentFiles, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}

			goAccessMu.Lock()
			previousFiles := goAccessLogFiles
			goAccessMu.Unlock()

			// Check if files have changed
			if !stringSliceEqual(previousFiles, currentFiles) {
				// Log the change
				fmt.Printf("[GoAccess] Log files changed: %d -> %d files\n", len(previousFiles), len(currentFiles))

				// Files changed, restart GoAccess
				goAccessMu.Lock()
				goAccessLogFiles = currentFiles
				needsRestart := goAccessRunning
				goAccessMu.Unlock()

				if needsRestart {
					fmt.Println("[GoAccess] Restarting due to log file changes...")
					restartGoAccess(workDir)
				}
			}
		}
	}
}

// restartGoAccess stops and restarts the GoAccess process
func restartGoAccess(workDir string) {
	goAccessMu.Lock()

	// Stop current process
	if goAccessCmd != nil && goAccessCmd.Process != nil {
		goAccessCmd.Process.Kill()
		goAccessCmd = nil
	}
	goAccessRunning = false

	goAccessMu.Unlock()

	// Wait a moment before restarting
	time.Sleep(1 * time.Second)

	// Start new process
	ensureGoAccessRunning(context.Background(), workDir)
}

// stringSliceEqual checks if two string slices are equal
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// goAccessStatusHandler provides debug information about GoAccess status
func goAccessStatusHandler(w http.ResponseWriter, r *http.Request) {
	goAccessMu.Lock()
	defer goAccessMu.Unlock()

	status := map[string]interface{}{
		"running":       goAccessRunning,
		"ws_url":        buildWebSocketURL(),
		"ws_port":       goAccessWSPort,
		"file_watcher":  goAccessWatchStop != nil,
		"tracked_files": len(goAccessLogFiles),
		"log_file_list": goAccessLogFiles,
	}

	if goAccessCmd != nil && goAccessCmd.Process != nil {
		status["pid"] = goAccessCmd.Process.Pid
	}

	// Check if GoAccess WebSocket is responding
	testURL := fmt.Sprintf("ws://localhost:%s", goAccessWSPort)
	conn, _, err := websocket.DefaultDialer.Dial(testURL, nil)
	if err != nil {
		status["ws_reachable"] = false
		status["ws_error"] = err.Error()
	} else {
		conn.Close()
		status["ws_reachable"] = true
	}

	// Check log file
	workDir, _ := os.Getwd()
	logFile := filepath.Join(workDir, "npm-data", "stats", "goaccess.log")
	if info, err := os.Stat(logFile); err == nil {
		status["log_file"] = logFile
		status["log_size"] = info.Size()
		status["log_modified"] = info.ModTime()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
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
	r.Get(goAccessWSRoute, goAccessWebSocketHandler) // WebSocket endpoint for real-time updates
	r.Get("/goaccess/status", goAccessStatusHandler) // Debug endpoint
}
