package api

import (
	"512SvMan/env512"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	goAccessWSAddr        = "127.0.0.1:7891" // GoAccess local WS
	goAccessPublicWSPath  = "/goaccess/ws"   // public WS path
)

var goAccessAllPanels = []string{
	"VISITORS",
	"REQUESTS",
	"REQUESTS_STATIC",
	"NOT_FOUND",
	"HOSTS",
	"OS",
	"BROWSERS",
	"VISIT_TIMES",
	"VIRTUAL_HOSTS",
	"REFERRERS",
	"REFERRING_SITES",
	"KEYPHRASES",
	"STATUS_CODES",
	"REMOTE_USER",
	"CACHE_STATUS",
	"GEO_LOCATION",
	"MIME_TYPE",
	"TLS_TYPE",
}

func isPortListening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// buildPublicWSURL builds the final ws/wss URL from MAIN_LINK and the WS path.
// - If MAIN_LINK uses https -> use wss
// - If MAIN_LINK uses http -> use ws
// - Removes/avoids duplicate slashes
func buildPublicWSURL(base, wsPath string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("MAIN_LINK is empty")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base // default if the scheme is missing
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid MAIN_LINK: %w", err)
	}
	if u.Host == "" && u.Path != "" {
		// Handles cases where something like "hyperhive.maruqes.com" ends up in Path
		reparse := "http://" + u.Path
		u, err = url.Parse(reparse)
		if err != nil {
			return "", fmt.Errorf("invalid MAIN_LINK (host): %w", err)
		}
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		// any other scheme (http, empty, etc.) uses ws
		u.Scheme = "ws"
	}

	// Normalize path: preserve any prefix and append /goaccess/ws
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Path = basePath + wsPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// --- GoAccess realtime ---

func ensureGoAccessDaemon() error {
	// Check/install goaccess if necessary (Fedora/RHEL via dnf)
	if _, err := exec.LookPath("goaccess"); err != nil {
		if _, derr := exec.LookPath("dnf"); derr != nil {
			return fmt.Errorf("goaccess not found and dnf not found either: %w", err)
		}
		if err := exec.Command("dnf", "-y", "install", "goaccess").Run(); err != nil {
			return fmt.Errorf("installing goaccess: %w", err)
		}
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}

	logDir := filepath.Join(workDir, "npm-data", "logs")
	statsDir := filepath.Join(workDir, "npm-data", "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return fmt.Errorf("stats dir: %w", err)
	}
	outputPath := filepath.Join(statsDir, "goaccess.html")

	// Adjust the glob pattern to your logs
	pattern := filepath.Join(logDir, "proxy-host-*_access.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("listing logs: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no logs found in %s", pattern)
	}

	// Nginx Proxy Manager log format
	logFormat := `[%d:%t %^] %^ %s %^ %^ %m %^ %v "%U" [Client %h] [Length %b] [Gzip %^] [Sent-to %^] "%u" "%R"`

	// Build the public WebSocket URL from MAIN_LINK
	publicWSURL, err := buildPublicWSURL(env512.MAIN_LINK, goAccessPublicWSPath)
	if err != nil {
		return err
	}

	// If it is already running and HTML exists, do not start another instance
	if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
		return nil
	}

	args := []string{}
	args = append(args, files...)
	args = append(args,
		"--no-global-config",
		"--date-format=%d/%b/%Y",
		"--time-format=%T",
		"--log-format="+logFormat,

		"--real-time-html",
		"--daemonize",
		"--persist",
		"--restore",

		"-o", outputPath,

		"--addr=127.0.0.1",
		"--port=7891",

		"--ws-url="+publicWSURL,
	)

	enablePanels := env512.GoAccessEnablePanels
	disablePanels := env512.GoAccessDisablePanels
	if len(enablePanels) == 0 && len(disablePanels) == 0 {
		enablePanels = goAccessAllPanels
	}

	for _, panel := range enablePanels {
		args = append(args, "--enable-panel="+panel)
	}
	for _, panel := range disablePanels {
		args = append(args, "--ignore-panel="+panel)
	}

	cmd := exec.Command("goaccess", args...)
	cmd.Dir = logDir
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("starting GoAccess realtime: %s", msg)
	}

	// Short wait for the WS/HTML to be ready
	for i := 0; i < 20; i++ {
		if isPortListening(goAccessWSAddr) && fileExists(outputPath) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("GoAccess started but WS/HTML are not ready")
}

// --- WS proxy and page ---

func newGoAccessWSProxy() http.Handler {
	u := &url.URL{Scheme: "http", Host: goAccessWSAddr}
	p := httputil.NewSingleHostReverseProxy(u)

	origDirector := p.Director
	p.Director = func(r *http.Request) {
		origDirector(r)
		r.URL.Scheme = "http"
		r.URL.Host = goAccessWSAddr
		r.URL.Path = "/" // GoAccess expects the root path
		r.Host = goAccessWSAddr
		// ReverseProxy keeps the upgrade headers
	}

	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "websocket proxy error: "+err.Error(), http.StatusBadGateway)
	}
	return p
}

func goAccessPageHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), goAccessTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- ensureGoAccessDaemon() }()

	select {
	case <-ctx.Done():
		http.Error(w, "timeout preparing GoAccess", http.StatusGatewayTimeout)
		return
	case err := <-done:
		if err != nil {
			http.Error(w, "GoAccess unavailable: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	workDir, _ := os.Getwd()
	html := filepath.Join(workDir, "npm-data", "stats", "goaccess.html")
	http.ServeFile(w, r, html)
}

func setupGoAccessAPI(r chi.Router) {
	// HTML page
	r.Get("/goaccess", goAccessPageHandler)
	// Public WebSocket -> proxy to 127.0.0.1:7891
	r.Handle(goAccessPublicWSPath, newGoAccessWSProxy())
}
